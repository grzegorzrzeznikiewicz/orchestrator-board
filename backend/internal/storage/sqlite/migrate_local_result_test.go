package sqlite

import "testing"

func TestMigration0200PreservesPRAndNumbersExistingLocalRounds(t *testing.T) {
	db := openMigratedDatabaseCopyNoForeignKeys(t, 199)
	_, err := db.Exec(`
INSERT INTO review (id,session_id,project_id,harness,pr_url,created_at,updated_at)
VALUES ('review','worker','project','claude-code','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO review_run (id,review_id,session_id,harness,pr_url,target_sha,target_kind,snapshot_id,status,verdict,body,created_at)
VALUES
 ('pr','review','worker','claude-code','https://github.com/acme/app/pull/1','head','pr','','complete','approved','PR history','2026-10-16 12:00:00'),
 ('local-a','review','worker','claude-code','','','local','snapshot-a','complete','approved','Local history A','2026-10-16 12:00:00'),
 ('local-b','review','worker','claude-code','','','local','snapshot-b','complete','changes_requested','Local history B','2026-10-16 12:00:00');`)
	if err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 200)
	for _, tc := range []struct {
		id, body, verdict string
		round             int
	}{
		{"pr", "PR history", "approved", 0},
		{"local-a", "Local history A", "approved", 1},
		{"local-b", "Local history B", "changes_requested", 2},
	} {
		var round int
		var body, verdict, result string
		if err := db.QueryRow(`SELECT round, body, verdict, local_result_json FROM review_run WHERE id=?`, tc.id).Scan(&round, &body, &verdict, &result); err != nil {
			t.Fatal(err)
		}
		if round != tc.round || body != tc.body || verdict != tc.verdict || result != "" {
			t.Fatalf("migration changed %s: round=%d body=%q verdict=%q result=%q", tc.id, round, body, verdict, result)
		}
	}
}
