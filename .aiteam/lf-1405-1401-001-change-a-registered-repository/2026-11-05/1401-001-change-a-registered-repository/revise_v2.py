import json

with open("result.json") as f:
    res = json.load(f)
req = res["result"]["payload"]["plan"]["storyrequest"]
req["expected_revision"] = "cc0f8d44d5ef38c21fab2888880bb689ff92486ac719ed8556e9c8feb9e29a3f"
arch = req["draft"]["architecture"]

def g(area, decision, rationale):
    return {"area": area, "decision": decision, "rationale": rationale}

resolver = g("Shared session resolver in the store (owner revision 2)",
    "Add one store-layer resolver, e.g. DB.SessionRepository(ctx, session) (Repo, error). It returns the snapshot's code target when a snapshot exists. Otherwise it returns the live repos row with base_branch replaced by the legacy base pin, when one exists. Otherwise it returns the live repos row unchanged. turn.executionRepository becomes a thin call to it, with no logic of its own. store.retirement_owners uses it directly instead of GetRepo for owner.Repo, because it is in the same package. projectconnection.Initialize uses it for each unpinned session's base when it builds that session's code version. The other session-scoped readers found by searching for GetRepo(…session.RepoID) also switch to it: feature_delivery fallback, daemon stage_decision_boundary, doctor files, doctor repair effects and maintenance cleanup. Owner-level readers with no session (repo list, feature drafts, owner actions, ticket recovery) keep reading the live row.",
    "The owner asked for this: one rule in the lowest layer that every caller already depends on. Putting it in turn would make store and daemon code import turn, or copy the rule. With the rule in one place, a reader cannot drift onto the live row, and retirement will never check an old issue against the new branch (O-05, K-02).")
safety = g("Safety net before switching readers",
    "Before any reader switches, add regression tests that pin today's behaviour for each switched reader. For a pinned session it returns the snapshot target. For an unpinned session with no legacy pin it returns the live row. Then add the resolver and switch the readers; the same tests must still pass. Last, add the tests for an unpinned session with a legacy pin (the pinned old base wins over the changed live row), including the retirement base guard and Initialize.",
    "Owner revision 2. Pinning today's behaviour first proves that the switch alone changes nothing. Only the pin case, which is new, changes a result. The tests use a real temporary database and fixture sessions, not mocks of the store.")
guard = g("repo add guard at the write boundary (O-08, K-01, owner revision 1)",
    "The guard lives in the store write, not only in the earlier read. Replace Register's UpsertRepo call (its only production caller) with one guarded store write, e.g. DB.RegisterRepo(ctx, repo, explicitBase bool). It runs in one BEGIN IMMEDIATE transaction and re-reads the row by path inside that transaction. New path: insert as today. Existing path with no --base: write every other field and keep the base stored at write time. That covers a `repo base` change committed after Register's earlier read. Existing path with an explicit --base that differs from the base stored at write time: return a typed refusal (base change via repo add) and write nothing. Explicit --base equal to the stored base: proceed as today. SeedRegistration runs only after a successful write. Register may still fail early on the earlier read for a clearer message, but the store check is the one that counts. Output prints the base that was actually stored.",
    "Owner revision 1: a check only at read time leaves a gap. If `repo base` commits between Register's read and its upsert, the upsert would overwrite the new base. With no flag, it would write back the old base, because Register fills the base in from that earlier read. Re-reading inside the write transaction closes the gap. It also fixes the existing problem where re-adding moved the record but not the connection (f3). K-01: first-time add and re-add with the same or no --base stay unchanged.")

out = []
for e in arch["general"]:
    a = e["area"]
    if a == "Readers that must use the issue's base":
        out += [resolver, safety]
        continue
    if a == "repo add guard (O-08, K-01)":
        out.append(guard)
        continue
    if a == "Older issues without a pinned copy (owner direction 2, K-02)":
        e["decision"] = e["decision"].replace(
            "executionRepository's fallback then uses the pinned base. Initialize uses the pinned base",
            "The shared store resolver (below) then returns the pinned base for that session, both at runtime and in Initialize, which uses that base")
    if a == "Shared places edited":
        e["decision"] = e["decision"].replace(
            "store/execution_connections, projectconnection import, localprojects registration,",
            "store repos write (guarded registration), a new store session resolver, store retirement_owners, turn/execution_connections, the switched session readers in turn, daemon, doctor and maintenance, projectconnection import, localprojects registration,")
    if a == "Tests and standards":
        old = "`repo add` base-change refusal, and an old unpinned issue"
        assert old in e["decision"]
        e["decision"] = e["decision"].replace(old,
            "`repo add` base-change refusal, an interleaving registration test (Register reads, then `repo base` commits, then Register writes: with no --base the new base survives; with a stale explicit --base the call is refused and nothing is written; the order is forced through a test hook or by calling the steps in sequence, never by timing), the resolver safety net above, and an old unpinned issue")
    out.append(e)
arch["general"] = out

sd = []
for c in arch["service_design"]:
    if c["component"] == "turn.executionRepository and projectconnection.Initialize":
        sd.append({"component": "store.SessionRepository (shared resolver)",
                   "responsibility": "The single rule for a session's repository: snapshot target, else live row with legacy pin base, else live row. turn.executionRepository delegates to it; retirement owners, Initialize and the other session readers call it.",
                   "access": "Store reads only (snapshot, pin and repos tables); no writes."})
        sd.append({"component": "store.RegisterRepo (guarded registration write)",
                   "responsibility": "Replaces UpsertRepo for Register. In one transaction it re-reads the row by path, keeps the stored base when no --base was given, and refuses an explicit base that differs from the stored one without writing.",
                   "access": "SQLite only; BEGIN IMMEDIATE through the existing connector."})
        continue
    sd.append(c)
arch["service_design"] = sd

for s in arch["solution_design"]:
    if s["element"] == "Base resolution for a session":
        s["contract"] = "base(session) = snapshot code version base, else legacy pin base, else live repos.base_branch. Implemented once, in store.SessionRepository; every per-session reader calls it, directly or through turn.executionRepository."
        s["rationale"] = "One rule in the lowest shared layer instead of a check at every reader (owner revision 2). This is what keeps already-started issues on their base (O-05, K-02)."
    if s["element"] == "Refusal types":
        s["contract"] = s["contract"].replace("base change via `repo add`.", "base change via `repo add` (returned by the guarded store write, so it also covers the race).")

res["result"]["comment"] = """I revised the technical candidate with your two points. Everything else stays as it was, including the small table for older issues. It is a draft for your review, not approved.

What changed:
1. Registration race: the `repo add` base guard now sits in the store write itself. Inside the same transaction, the write re-reads the stored base. With no `--base`, it keeps whatever base is stored at that moment, so a `repo base` that just committed is not overwritten. A stale or different explicit `--base` is refused and nothing is written. A new test forces the order "registration reads, `repo base` commits, registration writes".
2. Resolver: one store function now decides a session's repository: its pinned copy first, then the legacy base pin, then the live record. The existing execution helper only calls it. Retirement, daemon start-up and the other session readers use it too. Tests that pin today's behaviour for pinned and unpinned sessions come first; the readers switch after them.

```mermaid
flowchart LR
  R[session reader] --> S{store resolver}
  S -->|has pinned copy| A[pinned base]
  S -->|legacy pin| B[old base]
  S -->|neither| C[live record]
```

The size is still the set entry's M with "no migration". This revision adds the guarded write and the reader switch, so please weigh both when you re-estimate.

Next: approve this candidate, or tell me what to change."""

with open("result_v2.json", "w") as f:
    json.dump(res, f, ensure_ascii=False)
print("ok", len(json.dumps(res)))
