AI;DR: Validate owns the live L-story observation. Use the new team capture and retain the worker's tier, configured model and completed outcome together; local engineering tests do not certify this observation.

Use a review checkout containing the generated 1.0.1 catalogue. Run these commands and the test daemon with a binary built from that same checkout; changing the team alone does not add runtime support. Install and link from its local catalogue for the test repository; this does not require publishing a release:

```sh
litefactory team install go-development 1.0.1 --source /absolute/path/to/review-checkout/teams/teams.json
litefactory team inspect go-development 1.0.1 --source /absolute/path/to/review-checkout/teams/teams.json
litefactory team link go-development 1.0.1 --repo /absolute/path/to/test-clone --source /absolute/path/to/review-checkout/teams/teams.json
```

The equivalent new versions exist for dotnet-web, python-fastapi and swift-ios. Use the configured source explicitly if more than one catalogue offers that name and version. Existing running captures keep their original definition; linking a team does not replace them.

Choose a fresh approved L story whose saved sizing belongs to that exact parent, story and generation, then request Implement through its existing owner gate. Start a new capture after linking the updated team. Follow the story with `litefactory logs <story-issue> -f` and retain its completion evidence.

In the retained graph attempts, confirm that `sizing_valid` evaluated true, both S/M comparisons evaluated false and only `worker_flagship` ran. Its answer must flow through `worker_flagship_result` to successful saving and the done ledger state. Locate that attempt's run ordinal and its sibling `NNNN.meta.json` metadata document beside the run log. Inspect `delivery.evidence.specialists`: the selected worker records `tier: flagship` and its resolved configured `model`; `delivery.main_model` records the same selection. Keep the task, generation, approved revision, graph capture digest, worker attempt/run, tier/model and final completion together in the Validate record.

Record the configured model shown in retained evidence rather than assuming flagship names a specific provider model. This selection evidence does not establish provider-reported usage attribution. No owner observation or live provider run has been supplied during engineering. AC-006 remains pending until the owner performs and retains that check.
