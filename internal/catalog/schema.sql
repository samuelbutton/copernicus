CREATE TABLE scenarios (id TEXT PRIMARY KEY, content TEXT NOT NULL CHECK(json_valid(content))) STRICT;
CREATE TABLE controllers (id TEXT PRIMARY KEY, content TEXT NOT NULL CHECK(json_valid(content))) STRICT;
CREATE TABLE run_templates (id TEXT PRIMARY KEY, content TEXT NOT NULL CHECK(json_valid(content))) STRICT;
CREATE TABLE analysis_templates (id TEXT PRIMARY KEY, content TEXT NOT NULL CHECK(json_valid(content))) STRICT;
CREATE TABLE tests (
 id TEXT PRIMARY KEY,
 scenario_id TEXT NOT NULL REFERENCES scenarios(id),
 run_template_id TEXT NOT NULL REFERENCES run_templates(id),
 analysis_template_id TEXT NOT NULL REFERENCES analysis_templates(id)
) STRICT;
CREATE TABLE suites (id TEXT PRIMARY KEY) STRICT;
CREATE TABLE collections (id TEXT PRIMARY KEY) STRICT;
CREATE TABLE suite_tests (
 suite_id TEXT NOT NULL REFERENCES suites(id),
 position INTEGER NOT NULL CHECK(position >= 0),
 test_id TEXT NOT NULL REFERENCES tests(id),
 PRIMARY KEY(suite_id, position), UNIQUE(suite_id, test_id)
) STRICT;
CREATE TABLE collection_suites (
 collection_id TEXT NOT NULL REFERENCES collections(id),
 position INTEGER NOT NULL CHECK(position >= 0),
 suite_id TEXT NOT NULL REFERENCES suites(id),
 PRIMARY KEY(collection_id, position), UNIQUE(collection_id, suite_id)
) STRICT;
PRAGMA application_id = 1129333588;
PRAGMA user_version = 1;
