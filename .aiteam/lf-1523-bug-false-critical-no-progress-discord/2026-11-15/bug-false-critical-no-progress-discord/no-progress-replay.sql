-- Read-only replay of no_progress episodes since 2026-11-02 under the new rules.
WITH ep AS (
  SELECT json_extract(s.value,'$.Key') AS task,
         json_extract(e.value,'$.OpenedAt') AS opened,
         json_extract(e.value,'$.RecoveredAt') AS recovered
  FROM alert_ledger l, json_each(l.state,'$.Subjects') s, json_each(s.value,'$.Episodes') e
  WHERE l.id=1 AND json_extract(s.value,'$.Rule')='no_progress' AND json_extract(e.value,'$.OpenedAt')>='2026-11-02'
), cls AS (
  SELECT ep.*,
    CASE WHEN recovered LIKE '0001-%' THEN NULL ELSE round((julianday(recovered)-julianday(opened))*86400) END AS secs,
    (SELECT g.status||':'||g.current_node FROM sessions x JOIN graph_instances g ON g.session_id=x.id WHERE x.task_ref=ep.task ORDER BY g.id DESC LIMIT 1) AS latest
  FROM ep
)
SELECT CASE
   WHEN latest IN ('succeeded:delivered','succeeded:finished','succeeded:complete') THEN 'excluded: finished task'
   WHEN secs IS NOT NULL AND secs < 300 THEN 'excluded: shorter than 5 min'
   ELSE 'would still open' END AS verdict,
 count(*) AS episodes, group_concat(DISTINCT task) AS tasks
FROM cls GROUP BY verdict ORDER BY verdict;
