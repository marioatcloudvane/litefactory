-- Read-only replay of no_progress episodes since 2026-11-02 under the rules
-- of the fix: the finished-task check (as alertTaskTerminalSQL, without its
-- needs_poll=0 branch, since these flaps happened while needs_poll was set)
-- and the 5-minute open delay. Run: sqlite3 -readonly <factory db> < this file
WITH ep AS (
  SELECT json_extract(s.value,'$.Key') AS task,
         json_extract(e.value,'$.OpenedAt') AS opened,
         json_extract(e.value,'$.RecoveredAt') AS recovered
  FROM alert_ledger l, json_each(l.state,'$.Subjects') s, json_each(s.value,'$.Episodes') e
  WHERE l.id=1 AND json_extract(s.value,'$.Rule')='no_progress' AND json_extract(e.value,'$.OpenedAt')>='2026-11-02'
), cls AS (
  SELECT ep.*,
    CASE WHEN recovered LIKE '0001-%' THEN NULL ELSE round((julianday(recovered)-julianday(opened))*86400) END AS secs,
    coalesce((SELECT g.status||':'||g.current_node FROM sessions x JOIN graph_instances g ON g.session_id=x.id WHERE x.task_ref=ep.task ORDER BY g.id DESC LIMIT 1),'none') AS latest,
    (SELECT
       s.state<>'running'
       AND NOT EXISTS(SELECT 1 FROM runs r WHERE r.session_id=s.id AND r.status IN ('running','prelaunch'))
       AND NOT EXISTS(SELECT 1 FROM graph_instances g WHERE g.session_id=s.id AND g.id=(SELECT max(i.id) FROM graph_instances i WHERE i.session_id=s.id) AND g.status='running')
       AND NOT EXISTS(SELECT 1 FROM schedules p WHERE p.session_id=s.id AND p.status='pending')
       AND NOT EXISTS(SELECT 1 FROM automatic_continuations c WHERE c.session_id=s.id AND c.status='pending')
       AND (EXISTS(SELECT 1 FROM graph_instances g WHERE g.session_id=s.id AND g.id=(SELECT max(i.id) FROM graph_instances i WHERE i.session_id=s.id) AND (g.status IN ('succeeded','cancelled') OR (g.status='failed' AND g.current_node='owner_close'))
             AND NOT EXISTS(SELECT 1 FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id WHERE o.session_id=s.id AND o.assigned_attempt_id IS NULL AND c.classification='instruction' AND max(c.created_at,c.recorded_at)>COALESCE(g.finished_at,'')))
         OR (NOT EXISTS(SELECT 1 FROM graph_instances g WHERE g.session_id=s.id)
             AND NOT EXISTS(SELECT 1 FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id WHERE o.session_id=s.id AND o.assigned_attempt_id IS NULL AND c.classification='instruction' AND max(c.created_at,c.recorded_at)>COALESCE((SELECT max(r.finished_at) FROM runs r WHERE r.session_id=s.id),''))))
     FROM sessions s WHERE s.task_ref=ep.task) AS finished
  FROM ep
)
SELECT CASE
   WHEN coalesce(finished,0)=1 THEN 'excluded: finished task'
   WHEN secs IS NOT NULL AND secs < 300 THEN 'excluded: shorter than 5 min'
   ELSE 'would still open' END AS verdict,
 count(*) AS episodes,
 group_concat(DISTINCT task||' ['||latest||']') AS tasks
FROM cls GROUP BY verdict ORDER BY verdict;
