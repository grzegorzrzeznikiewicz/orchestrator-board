// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package sqlite

import "testing"

func TestMigration0201PreservesReviewsAndEmitsWorkflowCDC(t *testing.T) {
	db := openMigratedDatabaseCopyNoForeignKeys(t, 200)
	_, err := db.Exec(`INSERT INTO review(id,session_id,project_id,harness,pr_url,created_at,updated_at) VALUES('r','s','p','claude-code','',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO review_run(id,review_id,session_id,harness,target_kind,status,verdict,body,round,local_result_json,created_at) VALUES('local','r','s','claude-code','local','complete','approved','local history',2,'{"version":1}',CURRENT_TIMESTAMP),('pr','r','s','claude-code','pr','complete','approved','pr history',0,'',CURRENT_TIMESTAMP);`)
	if err != nil {
		t.Fatal(err)
	}
	upTo(t, db, 201)
	var n int
	if err = db.QueryRow(`SELECT count(*) FROM review_run WHERE verdict='approved' AND ((id='local' AND round=2 AND local_result_json='{"version":1}') OR (id='pr' AND body='pr history'))`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("history changed: %d %v", n, err)
	}
	_, err = db.Exec(`INSERT INTO local_workflow(id,session_id,request_id,version,phase,record_json) VALUES('wf','s','req',1,'waiting_executor','{}'); UPDATE local_workflow SET version=2,phase='testing' WHERE id='wf';`)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM change_log WHERE event_type='session_updated' AND json_extract(payload,'$.workflowId')='wf'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("CDC events=%d err=%v", n, err)
	}
}
