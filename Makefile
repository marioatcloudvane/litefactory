GO      ?= go
BINARY  := litefactory
DIST    := dist
PKGS    := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

# -trimpath keeps worktree paths out of the Go build cache keys, so every task
# worktree of the same commit reuses one set of cache entries instead of
# compiling and caching its own copy. Exported so test, vet and lint inherit it.
GOFLAGS += -trimpath
export GOFLAGS

# `check` runs its steps in order and stops at the first failure; running them
# concurrently would interleave their output for no gain.
.NOTPARALLEL:

.PHONY: all build test test-fast check check-fast fmt vet lint clean

all: build

## build: compile a static, cgo-free binary into dist/
build:
	@mkdir -p $(DIST)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BINARY) ./cmd/litefactory

## test: all tests, with race coverage concentrated on shared-state behavior
# All packages except the SQL-heavy suites retain full race coverage, including
# daemon dispatch, submission, mcpadapter, harness, codex and gittest. Instrumented
# pure-Go SQLite dominates turn/store/cli: run their full suites without -race,
# then the existing concurrency tests below with it. When adding concurrency in
# these packages, extend the selections. No tests are omitted from the full run.
# These conservative lists include all tests in files with goroutine or sync
# fixtures, plus the shared-state owners (attempt authority, submission sessions,
# progress/status, validation runtime, activity and meter snapshots).
TEST_TIMEOUT ?= 0
empty :=
space := $(empty) $(empty)
MIXED_PACKAGES := ./internal/turn ./internal/store ./internal/cli
RACE_TESTS_store := \
	TestStageDecisionRecoveryRacesAtomicCommit \
	TestStageDecisionRecoveryFenceAcrossIndependentConnections \
	TestDiscussionQueuedCancelAndFencedRunningCompletion \
	TestDiscussionAdmissionAndPublicationCheckConsentAtAtomicBoundary \
	TestTaskAdvisoryResultReplayAndLateWorkerFence \
	TestTaskAdvisoryInvocationCannotLaunchTwice \
	TestTaskAdvisoryStopBeforeLaunchPreventsPaidInvocation \
	TestDurableWorkflowConcurrentContinuationAdmission \
	TestOwnerActionAdmissionCancellationRace \
	TestOwnerActionReactionCancellationRace \
	TestPlanGroupMemberIsolationAndCompletion \
	TestPlanGroupMemberSharedResultWritersAndStaleParent \
	TestPlanGroupMemberAllocationRollbackAndPlanOnly \
	TestPlanGroupMemberRecoveryRetainsAcceptedResultsAndExpiresOrphans \
	TestPlanGroupMemberSessionReferenceUsesReservedOrdinal \
	TestOtherWorkerTimeoutDoesNotHoldImplementStory \
	TestWorkerTimingSnapshotSurvivesReplay \
	TestImplementTimeoutPendingReplyResumesExactlyOnce \
	TestImplementTimeoutCannotBypassAuthenticatedResume \
	TestAttemptTimeoutAtomicallyHoldsItsImplementWorker \
	TestAttemptTimeoutPreservesSubsecondParentAllowance \
	TestImplementTimeoutReplyUsesObservedRemoteNoticeTime \
	TestImplementTimeoutEqualTimestampNeedsObservedNoticeOrder \
	TestHeldRunningStoryDoesNotConsumeImplementCapacity \
	TestRestartDowntimePreservesExpiredWorkerDeadline \
	TestStandardsExceptionsTransitionsIndependentConnections \
	TestStandardsRunCompetingBindingsAndSelectionVersions \
	TestStandardsCompetingPublications \
	TestConversationCaptureConcurrentWinnerAndReplacement \
	TestApprovalIsAtomicAndExcludesPendingRefinements \
	TestReplanPreservesCompletedAndOldApprovedScope \
	TestCommentReservationCrossConnectionAndReopen \
	TestFamilyLedgerConcurrentReopenAndIsolation \
	TestGraphTriggerBandIsDisjoint \
	TestGraphDispatchLifecycle \
	TestParkRefusesADisagreeingNode \
	TestGraphDispatchGivesUpAfterRepeatedFailures \
	TestGraphDispatchRetryDelayGrows \
	TestGraphDispatchStopOutcomeIsOnlyActualExhaustion \
	TestGraphReplyRecoveryAtomicallyConsumesInput \
	TestImplementProgressSequenceValidationAndReceipt \
	TestImplementProgressAtomicFailureAndLostReply \
	TestImplementProgressAuthority \
	TestImplementProgressBudgetAndConcurrentRetry \
	TestImplementProgressLatestRestartAndAdvisoryOnly \
	TestImplementProgressMembershipWithRetainedGeneration \
	TestImplementProgressReceiptSurvivesCompaction \
	TestImplementStatusLockIsIndependentAndSerializesAliases \
	TestMeterPriceCatalogImmutableAndAtomic \
	TestMeterValuationSnapshotAndPin \
	TestMeterConcurrentCatalogEdits \
	TestMeterDiagnosticDedupBeforeBound \
	TestMeterRejectsMalformedFrozenValuations \
	TestMeterReplayAndSnapshot \
	TestMeterAtomicRollbackAndConstraints \
	TestMeterAttemptAndFinalization \
	TestMeterSnapshotLimitsAndCancellation \
	TestMeterSnapshotExactTimeBounds \
	TestMeterOutOfOrderAndCrossAttempt \
	TestMeterCheckpointAndRollback \
	TestMeterConcurrentAttempts \
	TestMeterQueryIndexes \
	TestMeterDiagnosticUnionAndProvenance \
	TestMeterPopulatedMigrationAndReopenReplay \
	TestMeterJoinedQueryWithRealisticCardinality \
	TestMeterFilteredIdentityCollisionAndCancelledCollection \
	TestReportDeliveryHTTPAndSQLiteFailureWindows \
	TestCreationAcceptanceHTTPAndSQLiteFailureWindows \
	TestResultBinding_AllModesAndContextContinuation \
	TestResultBinding_ModeTaskAndVersionIsolation \
	TestResultRecord_ConcurrentCASAndRollback \
	TestResultRequestReferences_OrderedBoundedAndScoped \
	TestResultHistory_ReopenReplayCollisionAndObservations \
	TestResultRecord_RejectionAndV2Isolation \
	TestRunLifecycleStorageBoundaries \
	TestForeignKeysAreEnforced \
	TestOpenRejectsAMissingDirectory \
	TestMigrateIsIdempotent \
	TestPollCursorTableHoldsOneRow \
	TestTimeRoundTripsAndSortsLexicographically \
	TestUpsertIdentityRefreshesWithoutDuplicating \
	TestUpsertRepoIsIdempotentOnPath \
	TestGetOrCreateSessionIsStable \
	TestFreezeSessionSlugKeepsTheFirstValue \
	TestSessionWorktreeAndState \
	TestSessionLockIsAtomic \
	TestAcquireSessionLockOnAMissingSession \
	TestSessionNeedsPollRoundTrips \
	TestListBacklogSessionsIsCrossRepo \
	TestNeedsPollSurvivesAReMigration \
	TestBootstrapWatermarkDoesNotConsumeComments \
	TestAdvanceWatermarkNeverRewinds \
	TestRecordCommentIsIdempotentAndTagsThirdParties \
	TestNextCommentTagCountsNumerically \
	TestPollCursorSurvivesAReopen \
	TestGetOrCreateSegmentReusesTheConversation \
	TestGetOrCreateSegmentRequiresAgentKind \
	TestSetSegmentResumeTokenNeverOverwritesANonEmptyToken \
	TestSetSegmentResumeTokenRejectsBadInput \
	TestSetSegmentAgentSwitchesInPlaceTransactionally \
	TestSetSegmentAgentRejectsBadInput \
	TestAllocateRunAssignsOrdinalsAndRefusesReplay \
	TestAllocateRunTakesTheIssueBodyAsTriggerZero \
	TestFinishRunComputesDuration \
	TestFinishRunRecordsAKillWithAReason \
	TestRecordRunSyncRoundTripsEveryOutcome \
	TestRecordRunSyncRefusesACommitShaOutsideSynced \
	TestRecordRunSyncOnAMissingRunReportsNotFound \
	TestCompleteRunReportIsOneTransaction \
	TestWithTxRollsBackEveryWrite \
	TestReconcileOrphans \
	TestFindSessionByIssueReportsAmbiguity \
	TestConcurrentAccessorsDoNotDeadlock \
	TestSubmissionMigrationPreservesLegacyVersions \
	TestSubmissionContractAndProjectionImmutability \
	TestSubmissionConcurrentAllocationAndIdempotency \
	TestSubmissionStaleAuthorityAndRollback \
	TestSubmissionReceiptRecoveryKeepsIndependentStates \
	TestSubmissionRetainsImmutableReferencedEvidence \
	TestSubmissionRejectsUnsafeSnapshotFacts \
	TestSubmissionRestartRetainsDiagnosticsAndPendingEffects \
	TestSubmissionRollsBackCounterAndBudgetAfterInsertFailure \
	TestSubmissionRunBudgetRejectsOverflowAtomically \
	TestSubmissionAssessmentMarkerIsFollowupMaterialized \
	TestSubmissionAssessmentKeepsExistingV1FollowupDebt \
	TestSubmissionMarkerFollowupMigrationBackfills \
	TestFollowupListsExcludeSubmissionMarkerRows \
	TestSubmissionAttemptReplacementRevokesOldAuthority \
	TestWorkflowBoundaryCompetingAdmissionReopen \
	TestWorkflowBoundaryPriorEvidence \
	TestWorkflowBoundaryCancellationAndWriteFailure \
	TestWorkflowBoundaryAmbiguousPriorEvidenceStops \
	TestWorkflowBoundaryWorktreeCleanupRetainsAdmission
RACE_TESTS_turn := \
	TestTaskAdvisoryRunnerBoundBrokerAndLostReplyRecovery \
	TestPlanParallelGrowthReselectsInvalidatedStage \
	TestPlanParallelCachedProjectionRechecksGuards \
	TestPlanParallelEndToEnd_CancellationJoinsAllMembers \
	TestPlanParallelEndToEnd_UncertainEffectRecovery \
	TestPlanParallelEndToEnd_ConcurrentAgreementAndOwnerFinalStop \
	TestPlanParallelEndToEnd_MixedTwoSmallOneLarge \
	TestPlanParallelEndToEnd_DependencyWaitPreservesIndependentSibling \
	TestPlanParallelEndToEnd_DurableSignalsAndRestart \
	TestPlanParallelEndToEnd_NormalSizingKeepsEveryOwnerReview \
	TestPlanParallelRawFacts \
	TestPlanParallelBasisInvalidation \
	TestPlanParallelBasisIndependentSibling \
	TestPlanParallelBasisPreservesAppliedProjection \
	TestPlanParallelContributionWireRoundTrip \
	TestPlanParallelCapacityAtomicBundle \
	TestGraphTimeoutSettlementRestoresMissingStopBeforeRetry \
	TestImplementTimeoutRedactsBeforeLedgerClipping \
	TestNonGraphFinalizationBoundsBlockedTransportJoin \
	TestSubmissionWorkerTimingFollowsTypedPlanPromptBinding \
	TestGraphParentDeadlineTimeoutSettlesAndKeepsDiagnostic \
	TestGraphParentDeadlineKeepsCleanupFailureWhenHookCannotSettle \
	TestAgentBudgetCancelsTheRunAndRecordsATimeout \
	TestAgentBudgetDoesNotTouchARunThatFinishes \
	TestAgentBudgetShutdownIsNotATimeout \
	TestValidateAfterAgentFailureSettlesFailedButNotShutdown \
	TestWorkerDeadlineSetupExpiryWithoutProcessResult \
	TestWorkerDeadlineParentCancellationIsNotTimeout \
	TestWorkerDeadlineRetainsPersistedTimingOnReplay \
	TestWorkerDeadlineNodeOverrideSurvivesShortProviderFallback \
	TestWorkerDeadlineCleanupHangAndFailure \
	TestWorkerDeadlineParentExpiryIsTimeout \
	TestWorkerSubmissionTimeoutRetainsActualReceiptAcrossRestart \
	TestWorkerDeadlineAcceptedResearchSurvivesExpiry \
	TestSlotCapacityStaysReservedThroughWorkflowFinalization \
	TestWorkflowJudgeStartupRecoveryBeforeGenericReconciliation \
	TestSlotCapacityWorkflowWaitsAndResumesWithoutReplay \
	TestStandardsEndToEndRetainedAcrossAttempts \
	TestStandardsToolsRejectForgedScopeAndReverifyReplay \
	TestStandardsEncodedMetadataPaging \
	TestActivitySocketBoundsAndOrdering \
	TestActivitySocketDurableRetryAndLifecycle \
	TestActivitySocketConcurrentRetry \
	TestActivitySQLiteUpdateBudgetAndReplayWindow \
	TestActivitySocketRevokedCallsHaveNoEffect \
	TestActivityService_LifecycleRetention \
	TestActivityService_LifecycleDoesNotRenewOrReopen \
	TestActivityService_LifecycleClonesViews \
	TestActivityService_LifecycleRequiresTime \
	TestActivityService_LifecycleConcurrentCopies \
	TestActivityService_EmptyLifecycle \
	TestAggregateLifecycle_BothWorkflowVersionsAllStagesCorrectionAndRestartRecovery \
	TestAggregateLifecycle_BothSyntheticAdapterPathsPerformSameRunCorrection \
	TestAggregateLifecycle_256CriterionAssessmentThroughBroker \
	TestAggregateLifecycle_LegacyContentVersionSkipsTypedBroker \
	TestExecuteRefusesASimultaneousSecondCallAtOneSession \
	TestReportCreationPresentationPostsOnceWithoutTouchingTheRunResult \
	TestReportCreationPresentationDoesNotConsumeAFreshOwnerComment \
	TestReportCreationPresentationRecoversPostThenCrashWithOneComment \
	TestReportCreationPresentationFailureLeavesItOwed \
	TestReportCreationPresentationFinalisesDeliveryCompletedByReportRecovery \
	TestReportCreationPresentationSkipsLegacyProbeButOrdinaryRunlessRepliesKeepIt \
	TestReportCreationPresentationNothingOwed \
	TestCreationHTTPLostReplyReopenObservation \
	TestCreationHTTPObservationRequiresExactUnambiguousProvenance \
	TestPlanDeliverySelectedRosterAndIdentity \
	TestDeliveryConcurrentPublicationIsDisjoint \
	TestVerifyDeliveryRejectsAlteredResources \
	TestDeliverySegmentRetainsAndRotatesExactIdentity \
	TestDeliverySegmentSwitchReconcilesDeliveryForTheNewAgent \
	TestExecuteFullTurnLeavesTheCloneUntouched \
	TestExecuteCommitCarriesTheTrailersAndStripsForgeries \
	TestExecuteNoChangesDoesNotCommitOrPush \
	TestExecuteAllocatesTheRunRowBeforeTheSubprocess \
	TestExecuteMarksTheSessionRunningForTheWholeDispatchWindow \
	TestExecuteReleasesTheLockOnPanic \
	TestExecuteReleasesTheLockOnAFailedRun \
	TestExecuteSwitchesTheSegmentInPlaceWhenTheResolvedAgentChanges \
	TestExecuteMakesNoSegmentWriteWhenTheAgentAlreadyMatches \
	TestExecuteSwitchAlwaysColdStartsRegardlessOfPriorSameAgentHistory \
	TestExecuteRefusesAnAgentSwitchOnlyForAContinuation \
	TestExecuteInFlightTurnFinishesUnderOldAgentThenTheNextTurnSwitches \
	TestExecuteLeavesTheCommentUnconsumedWhenLocked \
	TestExecuteReplayIsANoOp \
	TestExecuteFetchFailureAbortsWithNoWorktree \
	TestExecuteRecordsTheFetch \
	TestExecuteFreezesTheSlugThenReusesIt \
	TestExecuteRefusesACorruptedFrozenSlug \
	TestExecuteRefusesAnOccupiedWorktreePath \
	TestExecutePushFailurePreservesTheCommit \
	TestExecuteRecordsADistinctReasonPerOutcome \
	TestExecuteDoesNotCommitAnAbandonedRun \
	TestExecuteReportsAClaudeThatNeverStarted \
	TestExecuteResolvesTheAuthorFromTheClone \
	TestExecuteValidatesItsArguments \
	TestReconcileReclaimsAndLogs \
	TestOwnerStopSavesCancelledWritableRun \
	TestOwnerStopPlanDoesNotPublishFailure \
	TestExecuteRequiresACloneGate \
	TestCriticalSectionsAreSingleAcquisitions \
	TestTurnImposesNoAmbientDeadline \
	TestClaudeRunsOutsideTheCloneGate \
	TestConcurrentTurnsAgainstTwoClones \
	TestImplementReadyStartsAtMostFourInPlanOrder \
	TestImplementReadyHonoursMergedPrerequisites \
	TestImplementReadyDrainsWhenNothingCanEverStart \
	TestImplementReadyUnknownPrerequisiteNeverStarts \
	TestImplementActivitiesRefuseForeignModeAndBadAuthority \
	TestImplementMergeBusyLockReturnsRetryAndChangesNothing \
	TestImplementMergeNoneWhenNothingIsDone \
	TestImplementMergeTakesTheOldestDoneStoryOnePerCall \
	TestImplementMergePushesWithTheExpectedHeadAndRunsNoChecks \
	TestImplementMergeParksWithAPlainReason \
	TestImplementMergeNextDoneStoryGetsItsTurnAfterAPark \
	TestImplementMergeTransientFaultsDoNotPark \
	TestImplementMergeMovedHeadRetriesWithoutParkingOrNote \
	TestImplementMergePostsExactlyOneMarkedNoteAndReusesIt \
	TestImplementMergeFinishesAfterALostReplyWithoutAPostOrAGitCall \
	TestImplementHandoffPostsOnceThenEditsInPlace \
	TestImplementHandoffStaysWithin64KiB \
	TestImplementHandoffShowsStoriesTheLedgerDoesNotHoldYetAsWaiting \
	TestRegisterImplementActivitiesFillsBothRegistriesWithoutAPolicy \
	TestRegisterImplementActivitiesRequiresEveryDependency \
	TestImplementMergeUnreadableMigrationPreventsPublication \
	TestImplementStatusActivityIsCurrentAndAdvisory \
	TestImplementStatusProgressSafety \
	TestImplementStatusRedactsBeforeClippingAndEscaping \
	TestImplementStatusMissingProgressStillRendersLedger \
	TestImplementStatusRestartDeletionDuplicatesAndConcurrentRefresh \
	TestImplementStatusFailureCatchUpAndFinalError \
	TestImplementStatusAlreadyComplete \
	TestImplementStatusCoalescesTrailingAndStateSkipsGap \
	TestImplementStatusAcceptedProgressDoesNotFailOrWakeGraph \
	TestImplementStatusWorkerChangesRemainSuccessfulOnDisplayFailure \
	TestImplementStatusProgressBodyBudget \
	TestImplementStatusReadyStartsLiveCommentAndRebuildsLatestProgress \
	TestImplementStatusProductionBindingIncludesApprovedWithoutScheduling \
	TestCommentHTTPConcurrentRetries \
	TestCommentHTTPRefusesInvalidTargetsAndPreparedContent \
	TestCommentHTTPLostResponseReopenAndExactObservation \
	TestCommentHTTPCancellationRetainsIntentBeforeDispatch \
	TestMCPAttempt_PrivateSocketRevocationAndRecovery \
	TestMCPAttempt_RevokeBeforeAdmissionAndAfterRecordedWork \
	TestMCPAttempt_FreshLaunchBindingsAndCopiedScope \
	TestCodexMCPLifecycleEnabledDisabledAbsentSetup \
	TestCodexMCPLifecycleMalformedQuotedValuesSurvive \
	TestCodexMCPLifecycleServerCollisionRefusesSafely \
	TestCodexMCPLifecycleRunPathsPreserveRegistration \
	TestCodexMCPLifecycleConcurrentCancellationIsolation \
	TestMCPFamilyCompleteCatalogPipeSocketHTTP \
	TestMCPRuntimeActualLaunchPaths \
	TestMCPRuntimeConcurrentCancellationIsolation \
	TestMCPRuntimeReplacementRejectsStaleRegistration \
	TestMCPRuntimeApplicationGuidanceSizes \
	TestMCPTools_ModeServiceSocket \
	TestMCPTools_RevokedSocketDeniesBackend \
	TestMCPTools_PlanUnsafeRouteAndRecovery \
	TestMCPTools_ValidateRetainedAssessment \
	TestMCPTools_CompletedPlanRefused \
	TestMCPTools_EditSelectorAndPermissionBinding \
	TestMeterPersistsDistinctFallbackAttempts \
	TestPlanGraphBoundary_ProgressiveHandoffAndSave \
	TestPlanGraphBoundary_DirectEditsPreserveAndReopen \
	TestPlanGraphBoundary_AgentSuppliedFactoryValuesRefused \
	TestPlanGraphBoundary_SubmittedResultStandsWhateverIsPrinted \
	TestPlanGraphBoundary_TerminalOnlyOutputCannotCreateStories \
	TestPlanGraphBoundary_LostCreateAndAttachmentRecovery \
	TestPlanGraphBoundary_ThreeIndependentStories \
	TestPlanGraphBoundary_ChildAuthorityAndMixedFeedback \
	TestPlanGraphBoundary_SaveFailureKeepsPublicationUnready \
	TestPlanGraphBoundary_LostUpdatePreservesIdentity \
	TestPlanGraphBoundary_OwnerRetirementExcludesOnlyConfirmedScope \
	TestPlanGraphBoundary_PartialHandoffResumesSameIssues \
	TestPlanGraphBoundary_LaterNodesReadTheRetainedSharedResult \
	TestPlanGraphBoundary_AskPublishesQuestionsWithoutAStoryEffect \
	TestGraphPlanLostReply_RecoversAcceptedCandidateBeforeRetry \
	TestGraphPlanLostReply_RecoversAcrossDaemonRestart \
	TestGraphPlanLostReply_UncertainRecoveryStaysUncertain \
	TestGraphPlanLostReply_CorrectedRejectionUsesNewIdentity \
	TestPlanNodeGuidance_TellsAgentToRecoverBeforeRetry \
	TestPlanBoundaryCreateRecovery \
	TestPlanBoundaryUpdateAndRetirementRecovery \
	TestPlanBoundaryRejectsDriftAndUnauthorized \
	TestPlanBoundaryScopedArchitecturePersistsAcrossCycles \
	TestPlanBoundaryConcurrentUpdateIsUncertain \
	TestPlanBoundaryOversizedDraftPostsPartComments \
	TestPlanStoryLockWait \
	TestPlanBoundarySiblingLockWait \
	TestPlanBoundaryLockCancellation \
	TestPlanStoryHeldLockExpires \
	TestReportRunPostsThenCommitsAllThree \
	TestReportPostFailureLeavesTheWatermarkAndTheRunUntouched \
	TestReportRenderFailurePostsNothingAndAdvancesNothing \
	TestReportUsesTheBotCredentialOnly \
	TestReportReplyAdvancesWithoutARunRow \
	TestReportFailedRunStillPostsAndAdvances \
	TestReportReadinessRefusalPostsWithNoRunRow \
	TestReportReplayOfAReportedRunIsANoOp \
	TestReportRunTriggeredByTheIssueBody \
	TestReportSkipsALockedTurn \
	TestReportConsumeAdvancesWithoutPosting \
	TestReportValidatesItsArguments \
	TestSpikeFixOnlySmallStartsTheWriteFixer \
	TestSpikeFixPassedRecordsFixedThenReportsOnce \
	TestSpikeFixFailedCheckIsUnverifiedWithoutIssueOrRetry \
	TestSpikeFixTooBigNotesThenFilesOneIssueThenReports \
	TestSpikeFixIssueFailureStillReportsUnconfirmed \
	TestSpikeFixBlockedWhenTheRecipeIsMissingOrNothingChanged \
	TestSpikeFixFixerFailureReportsOnceAndStops \
	TestSpikeFixDotSwapsChangeThePathWithoutGoChange \
	TestSpikeFixCheckBlockedByRunnerSetupTimeoutAndCancellation \
	TestSpikeFixRouteRefusesAFinishedFixWithoutAValidReport \
	TestSpikeFixIssueReplayRecoversByMarkerAndNeverRecreates \
	TestSpikeFixCheckReplayIsSafeAndDoesNotRestartTheFixer \
	TestStoryAcknowledgementRecoversAcrossIndependentReporters \
	TestStoryProgressClosesDuringHeldParentBeforeSave \
	TestStoryProgressResponseLossAndInvalidationSurviveNewService \
	TestStoryProgressRejectsMutableEvidenceAndConflictingReplay \
	TestStoryProgressPendingCloseBlocksReplanUntilDelivered \
	TestStoryProgressUnreadInputPreventsPendingClose \
	TestStoryProgressIndependentServicesSerializeIngestionAndDelivery \
	TestStoryFeatureCompletesOnlyAfterAggregateAndAllStories \
	TestValidationCheckpointReconcilesTerminal \
	TestSubmissionLiveCorrectionAndFactoryEnvelope \
	TestSubmissionRejectsChangedWorktreeAndUnauthorizedSelector \
	TestSubmissionLegacyDecisionRecovery \
	TestSubmissionReplacementAttemptRejectsOldAuthority \
	TestSubmissionBothSyntheticRunnerPathsObserveDecisionBeforeExit \
	TestSubmissionValidationConsumesRetainedEvidenceOnly \
	TestSubmissionV2AcceptedStoryPollHasNoValidationParserError \
	TestSubmissionReferenceReworkChecksCurrentBytes \
	TestSubmissionConcurrentStoriesKeepCorrelation \
	TestSubmissionFinalDrainRetainsPendingDecision \
	TestSubmissionTerminalValidationRequiresRecordedReceipt \
	TestSubmissionRecoveryFromFreshStoreConnection \
	TestConcurrentTurnsSyncIndependentlyOnOneClone \
	TestTypedPlanFactoryBindingAndCompleteSet \
	TestTypedPlanRejectsSemanticRedactionAndInjectedEnvelope \
	TestTypedCriterionSourceAuthorityAndLegacyDispatch \
	TestTypedPlanSubmissionBoundArtifactsAndPendingReadiness \
	TestValidateManualStartRunsSuiteReviewAndOutcomeToTheOwnerWait \
	TestValidateAutomaticStartReachesTheSameStateAsAManualStart \
	TestValidateRestartAfterTheEntryWasSavedStillStartsOnce \
	TestValidateRestartAfterAdmissionLosesNoStartAndMakesNoSecondRun \
	TestValidateRestartMidSuiteNeedsOneOwnerStartAndNeverTwoLiveRuns \
	TestValidateProductionRuntimeRunsTheGraphToTheOwnerWait
RACE_TESTS_cli := \
	TestInitCreatesTheHomeConfigAndDatabase \
	TestInitKeepsTheGenericWarningForARealFailure \
	TestInitStoresTheBotIdentity \
	TestInitNeverPrintsTheBotToken \
	TestInitRefusesWhenTheBotIsTheOwner \
	TestInitFailsWhenGHCannotAnswer \
	TestInitIsIdempotent \
	TestInitRejectsABrokenConfig \
	TestInitWarnsWhenTheBotTokenCannotBeResolved \
	TestFollowAppLog_StreamsAppendedRecord \
	TestFollowAppLog_TerminalEventDoesNotStopFollow \
	TestFollowAppLog_WaitsForFileThenStreams \
	TestFollowAppLog_ResumesAfterTruncation \
	TestLogsRendersTheEventStream \
	TestLogsRendersTheNewestRunByDefault \
	TestLogsOnlySelectsKinds \
	TestLogsRejectsAnUnknownKind \
	TestLogsGrepFiltersLines \
	TestLogsGrepRejectsABadPattern \
	TestLogsGrepAppliesToRaw \
	TestLogsMarksSubAgentActivity \
	TestLogsFullShowsWholeToolOutput \
	TestLogsRendersWhatItCannotDecode \
	TestLogsRenderedOutputIsUnredacted \
	TestLogsRendersCodexThroughTheSameView \
	TestLogsFollowRendersAsItStreams \
	TestLogsFollowRenderedNeverEmitsAPartialLine \
	TestLogsAcceptsATaskRef \
	TestLogsRunWithNoEventLogIsReportedNotFatal \
	TestLogsListsRunsNewestLast \
	TestLogsListsATaskWithNoRuns \
	TestLogsAmbiguousIssuePrintsCandidates \
	TestLogsRejectsAMalformedRepoFlag \
	TestLogsUnknownTaskIsCleanAndNonZero \
	TestLogsRejectsANonNumericIssue \
	TestLogsWithNoDatabaseIsAPrecondition \
	TestLogsRunSummary \
	TestLogsRunSummaryAgentAndSessionRef \
	TestLogsRunSummaryTodoWriteWorkedExample \
	TestLogsRunSummaryOmitsRequestedToolsWhenTheyMatchResolved \
	TestLogsRequestedTools \
	TestLogsRunSummaryWithoutMetadata \
	TestLogsUnknownRunOrdinal \
	TestLogsRawIsByteForByte \
	TestLogsRawDefaultsToTheNewestRun \
	TestLogsOutputIsUnredacted \
	TestLogsUsesNoDaemonIPC \
	TestLogsRunFlagSelectsTheRunAfterThePositional \
	TestLogsFollowStreamsFromALiveWriter \
	TestLogsFollowNeverEmitsAPartialLine \
	TestLogsFollowOnAFinishedRunPrintsAndExits \
	TestLogsFollowOnAFinishedRunPrintsAnUnterminatedTail \
	TestLogsFollowStopsOnAnInterrupt \
	TestLogsFollowWithoutAnEventLog \
	TestMCPCLI_ExplicitServeWithAutomaticPolicyDisabled \
	TestMCPCLI_StaleDescriptorRefusedBeforeHandshake \
	TestMCPCLI_CancellationReachesSharedService \
	TestWebhookKeychainServiceMatchesTheDaemon \
	TestRepoAddWithoutPublicURLSkipsAndSaysSo \
	TestRepoAddRegistersTheWebhookAndIsRepeatable \
	TestRepoAddKeepsTheRegistrationWhenTheWebhookFails \
	TestRepoAddWarnsAboutPlainHTTP \
	TestRepoAddMakesNoGitHubCallWhenTheSecretCannotBeStored \
	TestRepoWebhookRepairsOneAndAll \
	TestRepoWebhookArguments \
	TestStoryContractUpgrade_CLIBoundary \
	TestTeamCommandsOutsideRepository \
	TestTeamWorkflowValidationRefusesUnknownActionAndMissingResource \
	TestTeamSourceSettingsCannotBeSilentlyTruncated \
	TestMeterEndToEndSyntheticProviderCoverage \
	TestMeterEndToEndProjectAndFilterMatrix \
	TestMeterEndToEndFrozenPricingDuringSlowExport \
	TestMeterEndToEndCancelledSlowExportCleanup \
	TestMeterExportFiltersAndCoverage \
	TestMeterCSVTextRoundTrip \
	TestMeterSummaryUnknownModelRetained \
	TestMeterExportReleasesDatabaseBeforeWriter \
	TestMeterExportBrokenWriterAndCancellation \
	TestMeterCSVShortHeader \
	TestMeterExactTimeBoundsAndProjectIsolation \
	TestMeterTextDetailsExposeMeasurementsAndProvenance \
	TestTeamBindingProcessHelper

test:
	@set -e; \
	selected="$$($(GO) list $(PKGS))"; \
	mixed="$$($(GO) list $(MIXED_PACKAGES) | tr '\n' ' ')"; \
	full_race=""; mixed_selected=""; \
	for pkg in $$selected; do \
	  case " $$mixed " in \
	    *" $$pkg "*) mixed_selected="$$mixed_selected $$pkg" ;; \
	    *) full_race="$$full_race $$pkg" ;; \
	  esac; \
	done; \
	if [ -n "$$full_race" ]; then $(GO) test -race -timeout=$(TEST_TIMEOUT) $$full_race; fi; \
	if [ -n "$$mixed_selected" ]; then $(GO) test -timeout=$(TEST_TIMEOUT) $$mixed_selected; fi; \
	for pkg in $$mixed_selected; do \
	  case $$pkg in \
	    */turn) tests='$(subst $(space),|,$(strip $(RACE_TESTS_turn)))' ;; \
	    */store) tests='$(subst $(space),|,$(strip $(RACE_TESTS_store)))' ;; \
	    */cli) tests='$(subst $(space),|,$(strip $(RACE_TESTS_cli)))' ;; \
	  esac; \
	  $(GO) test -race -timeout=$(TEST_TIMEOUT) -run "^($$tests)"'$$' $$pkg; \
	done

## test-fast: run the test suite without the race detector
# Run every package without instrumentation for local iteration. This does
# not replace the selected concurrency coverage in `test` and `check`.
test-fast:
	$(GO) test -timeout=$(TEST_TIMEOUT) $(PKGS)

## check: everything CI enforces — formatting, vet, lint, race tests
check: fmt vet lint test

## check-fast: check without the race detector, for iterating before `check`
check-fast: fmt vet lint test-fast

## fmt: fail if any file is not gofmt-clean
fmt:
	@unformatted="$$(gofmt -l . )"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt: these files are not formatted:"; \
		echo "$$unformatted"; \
		echo "run: gofmt -w ."; \
		exit 1; \
	fi

## vet: run go vet
vet:
	$(GO) vet $(PKGS)

## lint: run golangci-lint at a pinned version
#
# The version is pinned here and run via `go run` rather than requiring a system
# install, so every machine and CI runner lints with the same rules — an
# unpinned linter turns a green build red on someone else's laptop. This is a
# build-time tool only: it is kept out of the module graph in tools/go.mod, so
# it contributes nothing to the shipped binary's dependencies. A locally
# installed golangci-lint is preferred when present, purely because it is much
# faster; both paths run the same pinned version.
# .golangci.yml is a v2-format config, so the linter must be v2 — note the /v2
# in the module path. Bumping this version without checking the config format
# still matches is the one way this target breaks confusingly.
GOLANGCI_VERSION ?= v2.13.1
GOLANGCI        := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

lint:
	@if command -v golangci-lint >/dev/null 2>&1 && \
	   golangci-lint --version 2>/dev/null | grep -q "$(patsubst v%,%,$(GOLANGCI_VERSION))"; then \
		echo "golangci-lint $(GOLANGCI_VERSION) (installed)"; \
		golangci-lint run $(PKGS); \
	else \
		echo "golangci-lint $(GOLANGCI_VERSION) (go run; slower — 'brew install golangci-lint' to speed this up)"; \
		$(GO) run $(GOLANGCI) run $(PKGS); \
	fi

## clean: remove build output
clean:
	rm -rf $(DIST)
