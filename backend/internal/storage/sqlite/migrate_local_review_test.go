package sqlite

import "testing"

func TestMigration0199PreservesPRReview(t *testing.T) {
	db := openMigratedDatabaseCopyNoForeignKeys(t, 198)
	_, err := db.Exec(`INSERT INTO review (id,session_id,project_id,harness,pr_url,created_at,updated_at) VALUES ('review','worker','project','claude-code','https://github.com/acme/app/pull/1',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO review_run (id,review_id,session_id,harness,pr_url,target_sha,status,verdict,body,created_at) VALUES ('run','review','worker','claude-code','https://github.com/acme/app/pull/1','old-head','complete','approved','Existing review',CURRENT_TIMESTAMP);`)
	if err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 199)
	var kind, url, sha, status, verdict, body string
	if err = db.QueryRow(`SELECT target_kind,pr_url,target_sha,status,verdict,body FROM review_run WHERE id='run'`).Scan(&kind, &url, &sha, &status, &verdict, &body); err != nil {
		t.Fatal(err)
	}
	if kind != "pr" || url != "https://github.com/acme/app/pull/1" || sha != "old-head" || status != "complete" || verdict != "approved" || body != "Existing review" {
		t.Fatalf("legacy run changed: %s %s %s %s %s %s", kind, url, sha, status, verdict, body)
	}
}
