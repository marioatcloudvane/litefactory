canonical creation and publication pure refusal race checks
go test ./internal/submission ./internal/turn -run TestCanonicalCreation_|TestCreationPublicationService_DecodeExactReviewedEnvelope|TestCreationPublicationService_PermissionMaterialExcludesPermit|TestOwnerGrantCommand_ExactPublicGrammar|TestCreationAuthorizer_RejectsUntrustedControlBeforeStore|TestCreationAuthorizer_GateAndUnassignedPermissions -short -race -cover -timeout=8m
Submission PASS coverage 2.9%; turn PASS coverage 0.6%. Three new functions, 27 subtests and existing targeted groundwork tests pass.

go test -race ./internal/turn ./internal/store -run ^TestCreationHTTP|^TestCreationPublicationDB -count=1
ok internal/turn 31.464s
ok internal/store 4.721s
Eight test functions pass. Exact bytes/confirmed IDs, concurrent retry, actual config.Load omitted/false gates with otherwise valid grants, expiry/revocation/mutations, cancellation, lost replies/reopen, partial relationships, authenticated observation and monotonic persistence.
Existing primitive tests supplement quota/provenance coverage; authentic host work remains #274.
