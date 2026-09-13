-- The caller supplies pairs(id, baseline_job, candidate_job) and
-- outcomes(json), read from exact validated result-file copies.
WITH metrics AS (
    SELECT p.id, name,
        (b.json->('$.metrics.' || name)) AS baseline,
        (c.json->('$.metrics.' || name)) AS candidate
    FROM pairs p
    JOIN outcomes b ON (b.json->>'$.job_id') = p.baseline_job
    JOIN outcomes c ON (c.json->>'$.job_id') = p.candidate_job
    CROSS JOIN (VALUES ('collision_count'), ('minimum_obstacle_gap'), ('goal_progress')) names(name)
), values_checked AS (
    SELECT id, name,
        baseline->>'$.unit' AS unit,
        CAST(baseline->>'$.version' AS INTEGER) AS version,
        (baseline->>'$.unit') IS NOT DISTINCT FROM (candidate->>'$.unit')
            AND (baseline->>'$.version') IS NOT DISTINCT FROM (candidate->>'$.version') AS compatible,
        CAST(baseline->>'$.value' AS BIGINT) AS baseline,
        CAST(candidate->>'$.value' AS BIGINT) AS candidate,
        CAST(baseline->>'$.pass' AS BOOLEAN) AS baseline_pass,
        CAST(candidate->>'$.pass' AS BOOLEAN) AS candidate_pass
    FROM metrics
)
SELECT id, name, unit, version, compatible, baseline, candidate,
    CASE WHEN compatible THEN candidate - baseline END AS delta,
    baseline_pass, candidate_pass,
    CASE
        WHEN NOT compatible THEN 'INCOMPARABLE'
        WHEN baseline IS NULL OR candidate IS NULL THEN 'UNAVAILABLE'
        WHEN baseline = candidate THEN 'UNCHANGED'
        WHEN (name = 'collision_count' AND candidate > baseline)
            OR (name <> 'collision_count' AND candidate < baseline) THEN 'REGRESSION'
        ELSE 'IMPROVEMENT'
    END AS change
FROM values_checked
ORDER BY id, name;
