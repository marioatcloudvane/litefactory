CREATE TABLE repository_team_bindings (
    repo_id INTEGER PRIMARY KEY REFERENCES repos(id),
    source TEXT NOT NULL,
    slug TEXT NOT NULL,
    version TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('fetched', 'local')),
    release_digest TEXT NOT NULL,
    CHECK(length(source)>0 AND length(slug)>0 AND length(version)>0),
    CHECK((kind='local' AND source='local' AND release_digest='') OR
          (kind='fetched' AND source<>'local' AND length(release_digest)=64))
);
CREATE INDEX repository_team_exact_use ON repository_team_bindings(source,slug,version);
