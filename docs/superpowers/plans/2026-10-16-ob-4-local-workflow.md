# OB-4 Local Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Recommended here: native execution with `executing-plans`; no subagents until the user chooses an execution method.

**Goal:** Automatycznie prowadzić istniejącą sesję Codex przez testy, lokalne review Claude i wymagane poprawki, z trwałą historią, limitem rund i świadomym wznowieniem.

**Architecture:** Jeden koordynator w demonie utrwala przejścia i rezerwacje operacji przed wykonaniem skutków ubocznych. Wykorzystuje snapshoty oraz protokół OB-2/OB-3 i obecne dostarczanie wiadomości do wykonawcy; nie uruchamia równoległego mechanizmu PR. Wyniki testów i review muszą dotyczyć tych samych plików przy zachowanej ciągłości obserwacji.

**Tech Stack:** Go i SQLite/sqlc w demonie; istniejące usługi agentów; generowane OpenAPI/TypeScript; React i Vitest w Electronie. Bez nowych zależności produktu, jeśli obecne narzędzia wystarczą.

**Spec:** [Zatwierdzona specyfikacja](../specs/2026-10-16-ob-4-local-workflow-design.md).

## Stan wykonania — 2026-10-17

Zadania implementacyjne 1–8 są wykonane. Integracyjna ścieżka z zadania 9 oraz poprawki po jednym niezależnym przeglądzie są wykonane. Bieżące wyniki kontroli i mapę testów zawiera [raport OB-4](../../ob-4-verification.md). Poniższa szczegółowa lista pozostaje zapisem zatwierdzonego planu; faktyczny zakres i rozstrzygnięcia dokumentuje raport.

Odstępstwa opisane w raporcie: istniejący typ CDC `session_updated`, rzeczywista lokalizacja adaptera frontendowego, bezpieczne zatrzymanie automatycznych poprawek bez trwałego dowodu zakończenia TUI oraz prywatny host komend. Po odblokowaniu Maca zaliczono natywny odbiór panelu/ustawień na kontrolowanych danych oraz integrację przechwytywania obrazu. Szczegółowy zakres i ograniczenie dotyczące rzeczywistych modeli zawiera raport.

Uwaga do publikacji: po zakończonym odbiorze użytkownik jawnie autoryzował commit, push, PR i merge. Ta późniejsza instrukcja zastępuje poniższy pierwotny zakaz publikacji; pozostałe ograniczenia pozostają w mocy.

## Global Constraints

- Rzeczywisty katalog `/Users/grzegorzrzeznikiewicz/Programowanie/GamaSoftware/OrchestratorBoard`, gałąź `feature/OB-4`, baza `c29627b` z OB-3.
- Bez dodatkowych worktree, commitów, push, PR ani publikacji. Zmiany OB-3 są bazą, nie zakresem OB-4.
- Limit domyślny **3**; wznowienie nie zeruje licznika; zwiększenie limitu jest jawne.
- Jedna sesja ma najwyżej jeden niezamknięty przebieg. Ustawienia, zadanie i komendy są kopiowane do przebiegu.
- Błąd testów, recenzenta, konfiguracji albo niepewna aktywność blokują przejście. Pusta lista testów blokuje workflow, ale pozostaje poprawna dla ręcznego review.
- Wykonawca nie edytuje podczas testów i review; nieznany probe nie dowodzi zakończenia pracy.
- Wszystkie logi i procesy pomocnicze używają katalogu danych AO. Testy nie migrują używanej bazy `~/.ao`.
- Migracja wyłącznie nowa; queries są źródłem dla sqlc; API jest generowane; CDC pochodzi z triggerów.
- Kolejność: test zachowania → potwierdzony oczekiwany błąd → implementacja → zaliczone testy. Każde zadanie kończy się sprawdzeniem diffu, bez commita.

## Review Focus

1. Dwa różne ID ponowionego startu dla tej samej sesji nie mogą ominąć ograniczenia jednego przebiegu; test w zadaniu 2.
2. Zmiana i przywrócenie pliku pomiędzy testami a snapshotem nie mogą dać gotowości do akceptacji; test w zadaniu 5.
3. Anulowana wiadomość nadal w kolejce nie może uruchomić poprawek po zatrzymaniu; test w zadaniu 4 i 6.
4. Awaria po uruchomieniu procesu, ale przed zapisem jego PID, nie może powodować ponownego startu ani sygnalizowania obcego procesu po recyklingu PID; test w zadaniu 3 i 6.
5. Zmiana trybu sesji lub generacji kontrolera w trakcie poprawek nie pozwala uznać obcego zakończonego obrotu za wynik workflow; test w zadaniu 4 i 6.

---

### Task 1: Konfiguracja i model trwałych faktów

**Files:** Modify `backend/internal/domain/projectconfig.go`, `backend/internal/domain/local_review_test.go`; create `backend/internal/domain/local_workflow.go` and `local_workflow_test.go`.

**Interfaces:**
- Dodaj `ProjectConfig.LocalWorkflowMaxRounds int` z JSON `localWorkflowMaxRounds,omitempty`; `EffectiveLocalWorkflowMaxRounds() int` zwraca 3 dla zera. `Validate()` odrzuca wartości ujemne. Wartość zero pozostaje zgodna ze starymi projektami.
- `LocalWorkflowRun`: `ID string`, `SessionID SessionID`, `Version int64`, `Phase LocalWorkflowPhase`, `Cycle int`, `MaxRounds int`, `Task string`, `LatestUserPrompt string`, `TestCommands []string`, `Env map[string]string`, `WorkspacePath string`, `WorkerGeneration string`, `StopRequested bool`, `BlockCode string`, `BlockMessage string`, `CreatedAt time.Time`, `UpdatedAt time.Time`, `ClosedAt *time.Time`.
- `LocalWorkflowPhase`: `waiting_executor`, `testing`, `reviewing`, `fixing`, `ready`, `blocked`, `stopping`, `stopped`. `ready` nadal podlega unieważnieniu; nowy przebieg wymaga jawnego zamknięcia poprzedniego przez start następnego, z zachowaniem historii.
- `LocalWorkflowOperation`: `ID`, `WorkflowID`, `Kind` (`tests`, `review`, `fix`), `Cycle`, `Attempt`, `State` (`reserved`, `running`, `succeeded`, `failed`, `interrupted`, `uncertain`), `Generation`, `ClientMessageID`, `WorkerTurnID`, `ReviewRunID`, `SnapshotID`, `ObservationID`, `ErrorCode`, `ErrorMessage`, `StartedAt`, `CompletedAt`.
- `LocalWorkflowTestResult`: `ID`, `OperationID`, `CommandIndex`, `Command`, `Status`, `ExitCode *int`, `StdoutLog`, `StderrLog`, `StartedAt`, `CompletedAt`. ID i ścieżki to string, indeksy/cykle/próby to int, czasy to `time.Time` lub `*time.Time` dla niezakończonych operacji.

- [ ] Napisz testy domyślnego limitu 3, dodatniego limitu, walidacji ujemnego, zachowania trybu PR i pustego planu testów dla ręcznego review.
- [ ] Uruchom `cd backend && go test ./internal/domain -run 'TestLocalWorkflow|TestLocalReviewProjectConfig' -count=1`; potwierdź błąd brakującego zachowania.
- [ ] Zaimplementuj typy i walidację bez logiki procesu w modelu projektu.
- [ ] Powtórz powyższe testy, następnie `go test ./internal/domain`.

### Task 2: SQLite, atomowe przejścia i rezerwacje

**Files:** Create `backend/internal/storage/sqlite/migrations/0201_local_workflow.sql`, `queries/local_workflow.sql`, `store/local_workflow_store.go`, `store/local_workflow_store_test.go`, `migrate_local_workflow_test.go`; modify `migrate_burned_versions_test.go`; regenerate `gen/`.

**Interfaces:** Store methods, all with `context.Context` first:
- `CreateLocalWorkflow(ctx, run domain.LocalWorkflowRun, requestID string) (domain.LocalWorkflowRun, bool, error)`.
- `GetLocalWorkflow(ctx, id string) (domain.LocalWorkflowRun, bool, error)`; `CurrentLocalWorkflow(ctx, sessionID domain.SessionID) (domain.LocalWorkflowRun, bool, error)`; `ListOpenLocalWorkflows(ctx) ([]domain.LocalWorkflowRun, error)`.
- `AdvanceLocalWorkflow(ctx, expectedVersion int64, next domain.LocalWorkflowRun, operation *domain.LocalWorkflowOperation) (bool, error)` atomowo publikuje zmianę i opcjonalną rezerwację.
- `CompleteLocalWorkflowOperation(ctx, expectedVersion int64, operation domain.LocalWorkflowOperation, next domain.LocalWorkflowRun, tests []domain.LocalWorkflowTestResult) (bool, error)` zapisuje wynik i następny etap tylko dla właściwej wersji.
- `ListLocalWorkflowOperations(ctx, workflowID string) ([]domain.LocalWorkflowOperation, error)`; `ListLocalWorkflowTests(ctx, workflowID string) ([]domain.LocalWorkflowTestResult, error)`.

- [ ] Dodaj testy round trip konfiguracji, zamknięcia/otwarcia prawdziwej bazy, unikalności jednego otwartego przebiegu także przy różnych request ID, konfliktu ponowionego request ID z innym payloadem oraz CAS dwóch konkurencyjnych przejść.
- [ ] Dodaj test migracji zachowującej lokalne i PR review OB-3 oraz test CDC `local_workflow_updated` ze zmianą etapu i wyniku testu.
- [ ] Uruchom skupione testy i potwierdź błąd, następnie dodaj trzy tabele: `local_workflow`, `local_workflow_operation`, `local_workflow_test`. Unikalne klucze: request startu, aktywna sesja, `(workflow_id, cycle, kind, attempt)`, identyfikator wiadomości poprawek. Wyniki testów i review są faktami, nie statusem sesji.
- [ ] Dodaj triggery CDC i indeksy. Aktualizacje wraz z rezerwacją wykonuj w transakcji pod istniejącym writer mutex; nie przenoś transakcji do koordynatora.
- [ ] Uruchom `npm run sqlc`, następnie `cd backend && go test ./internal/storage/sqlite/store -run LocalWorkflow -count=1 && go test ./internal/storage/sqlite -run Migration0201 -count=1`.

### Task 3: Komendy testów, logi i kontrola procesów

**Files:** Create `backend/internal/localworkflow/test_runner.go`, `test_runner_test.go`, `test_process_unix.go`, `test_process_windows.go`, `test_host.go`, `test_host_test.go`; modify `backend/cmd/ao/main.go` only for private test-host dispatch before public CLI execution.

**Interfaces:**
- `TestPlan` zawiera `OperationID string`, `WorkspacePath string`, `DataDir string`, `Commands []string`, `Env map[string]string`, `CommandTimeout time.Duration`.
- `TestRunner`: `Run(context.Context, TestPlan) ([]domain.LocalWorkflowTestResult, error)`, `Cancel(context.Context, operationID string) error`, `Reconcile(context.Context, operationID string) (TestProcessState, error)`.
- `TestProcessState` ma `State string` (`absent`, `running`, `settled`, `uncertain`) i `Results []domain.LocalWorkflowTestResult`.
- `RunTestHost(args []string) (handled bool, exitCode int)` obsługuje wyłącznie prywatny prefiks `--internal-local-workflow-test-host` i plik planu w katalogu operacji pod AO data dir. Nie rozszerza publicznych komend CLI o bezpośredni dostęp do usług lub SQLite.

- [ ] Napisz testy rzeczywistych małych subprocessów: kolejność komend, przerwanie po pierwszym błędzie, exit code, stdout/stderr, timeout, anulowanie drzewa procesu, spacje w katalogu, błąd otwarcia logu i brak sukcesu przy pustej liście.
- [ ] Napisz test ponowionego uruchomienia tej samej operacji oraz awarii przed/po starcie dziecka, także przed utrwaleniem PID; nie może powstać drugi proces. Test recyklingu PID ma dowodzić braku sygnału do obcego procesu.
- [ ] Potwierdź błędy przez `cd backend && go test ./internal/localworkflow -run 'TestTestRunner|TestTestHost' -count=1`.
- [ ] Wykonuj komendy powłoką platformy, po kolei, domyślnie z limitem 20 minut na komendę. Plan/spec nie ustala tego limitu: jest to jawna decyzja implementacyjna, zapisywana w planie wykonania testów. Nie wykonuj automatycznych retry.
- [ ] Host otrzymuje wyłączne trwałe oznaczenie startu operacji przed uruchomieniem komendy; duplikat odczytuje rezultat i nie wykonuje planu ponownie. Zapisuje prywatny rejestr procesów, wynik i logi poza repozytorium oraz odbiera anulowanie. Stan po awarii hosta, którego nie da się rozstrzygnąć, pozostaje `uncertain`; brak samego PID nie dowodzi braku dziecka. Przed sygnałem weryfikuj tożsamość procesu i kontroluj potomków przy użyciu istniejących wzorców `process`/`previewserver`.
- [ ] Ogranicz rozmiar zapisanych logów do 10 MiB na strumień i komendę, z oznaczeniem obcięcia; logi otwieraj przed spawn. Błąd zapisu nie może dać sukcesu. Nie przekazuj ścieżki pliku logu z żądania HTTP.
- [ ] Powtórz testy procesu i zbuduj pakiet dla Windows; wykonanie Windows musi zostać osobno zweryfikowane na Windows.

### Task 4: Wykonawca i wyłączność etapu

**Files:** Create `backend/internal/localworkflow/worker.go`, `worker_test.go`, `backend/internal/daemon/local_workflow_worker.go`; modify `backend/internal/ports/local_review.go`, `backend/internal/storage/sqlite/store/review_store.go`, `conversation_store.go`, `queries/local_workflow.sql`, focused session-manager/chat tests.

**Interfaces:**
- `Worker`: `Observe(context.Context, domain.SessionID, operationID string) (WorkerObservation, error)`, `DeliverFix(context.Context, domain.SessionID, operationID, text string) (WorkerObservation, error)`, `Interrupt(context.Context, domain.SessionID, operationID string) error`.
- `WorkerObservation` ma `State string` (`idle`, `queued`, `running`, `completed`, `interrupted`, `failed`, `uncertain`, `needs_input`), `TurnID string`, `Generation string`, `ClientMessageID string`, `Error string`.
- `CheckLocalWorkflowWorkerAdmission(ctx, sessionID domain.SessionID) error` sprawdza trwały etap przed przyjęciem pracy. Dopuszcza pracę w `waiting_executor`/`fixing`; odmawia w `testing`/`reviewing`/`stopping`. Sprawdzaj razem z wstawieniem wiadomości w tej samej transakcji oraz przed wysłaniem do TUI pod wspólną bramką. Nie trzymaj bramki podczas wywołania `SendSemantic`, który sam ją pobiera.

- [ ] Dodaj test dostarczenia poprawek z tym samym operation ID dwa razy: jedna wiadomość i jedna praca agenta. Sprawdź odzyskanie turn ID po awarii między akceptacją wiadomości a zapisem potwierdzenia workflow.
- [ ] Dodaj testy: wykonawca aktywny, unknown, oczekujący na zgodę, zakończony, przerwany, zmiana generacji, wiadomość w kolejce anulowana przed startem, równoczesna ręczna wiadomość podczas testów/review.
- [ ] Potwierdź błąd testów, następnie zbuduj adapter w daemonie nad `Manager.SendSemantic`, chat `Interrupt` i `Manager.InterruptTUI`; obserwuj trwały turn/checkpoint, nie sam napis odpowiedzi ani sam stan idle.
- [ ] Dla Chat używaj trwałego client message ID operacji i korelacji obrotu. Dla TUI używaj potwierdzenia semantycznego z właściwej generacji i potwierdzonego zakończenia; jeśli brakuje dowodu, zgłoś `uncertain` i nie wysyłaj ponownie automatycznie.
- [ ] Sprawdź, że queued fix zostaje trwale anulowany przed zgłoszeniem zatrzymania, a brak pewności anulowania blokuje wznowienie. Zachowaj istniejące zachowanie sesji bez workflow.
- [ ] Zregeneruj sqlc i uruchom skupione testy pakietów localworkflow, store, chat i session_manager, następnie ich pełne zestawy przed odbiorem.

### Task 5: Koordynator testy → review → poprawki

**Files:** Create `backend/internal/localworkflow/coordinator.go`, `coordinator_test.go`, `review.go`, `prompt.go`, `prompt_test.go`; modify `backend/internal/review/review.go`, `local_review.go` and tests; add narrow workflow methods in `backend/internal/service/review/`.

**Interfaces:**
- `Coordinator`: `Start(ctx context.Context, sessionID domain.SessionID, requestID string) (Snapshot, error)`, `Get(ctx context.Context, sessionID domain.SessionID) (Snapshot, error)`, `Evaluate(ctx context.Context, workflowID string) error`. `Snapshot` ma `Run domain.LocalWorkflowRun`, `Operations []domain.LocalWorkflowOperation`, `Tests []domain.LocalWorkflowTestResult`, `Current bool`, `RequiredFindings []domain.LocalReviewFinding`.
- `New(deps Deps) *Coordinator`; `Deps` zawiera store z zadania 2, `Worker`, `TestRunner`, `Reviews`, clock, data dir i logger. Wszystkie skutki uboczne są w wąskich zależnościach.
- `Reviews`: `TriggerWorkflowLocal(ctx context.Context, sessionID domain.SessionID, operationID, expectedSnapshotID string) (domain.ReviewRun, error)`, `GetWorkflowLocal(ctx context.Context, runID string) (domain.ReviewRun, error)`, `CancelWorkflowLocal(ctx context.Context, runID string) error`.
- `BuildFixPrompt(run domain.LocalWorkflowRun, result domain.LocalReviewResult) string`: wymagane uwagi i zadanie oraz zakaz commitów i PR; sugestie nie stają się wymaganiami.

- [ ] Napisz test pełnego cyklu: testy success, `changes_requested`, jeden fix, kolejne testy i `approved`; dopiero końcowe zgodne dowody dają `Current=true` i `ready`.
- [ ] Dodaj testy limitu 3, sugestii przy approved, pustej konfiguracji, niedostępnego Claude, błędu protokołu i błędu komendy bez startu review. Potwierdź oczekiwane błędy.
- [ ] Dodaj testy na prawdziwym tymczasowym repo: edycja podczas testów, zmiana/przywrócenie bajtów w przerwie przed snapshotem oraz edycja po zatwierdzeniu. Żaden przypadek nie daje aktualnego ready.
- [ ] Zaimplementuj przejścia warunkowe. Użyj `localsnapshot.Capture`, `Watch`, `Guard.Check`; obserwację utrzymuj od testów do zakończenia review, a nie osobno na każdy etap. Rezerwuj etap pod bramką wykonawcy przed zwolnieniem jej na czas procesów.
- [ ] Dodaj wewnętrzny trigger workflow do istniejącego review zamiast podszywać go pod ręczny HTTP trigger. Rezerwacja review operation ID musi być trwale powiązana z `review_run` przed spawn; po awarii odczytuje się tę samą rundę. Dotychczasowy automatyczny PR trigger pozostaje odrębny. Konfiguracja przebiegu określa wybór recenzenta.
- [ ] Przy zmianie etapu sprawdzaj generację, żądanie stop i zgodność snapshotu. Po `changes_requested` ostatniego cyklu utrwal `round_limit` i ostatnie wymagane uwagi, bez kolejnej wiadomości do Codexa.
- [ ] Uruchom `cd backend && go test ./internal/localworkflow ./internal/review ./internal/service/review`, następnie testy tych pakietów z `-race`.

### Task 6: Zatrzymanie, wznowienie i rekoncyliacja po restarcie

**Files:** Create `backend/internal/localworkflow/recovery.go`, `recovery_test.go`, `control.go`, `control_test.go`, `backend/internal/daemon/local_workflow_wiring.go`; modify `daemon.go`, API deps wiring, relevant daemon wiring tests.

**Interfaces:** `Coordinator.Stop(ctx context.Context, sessionID domain.SessionID, expectedVersion int64) (Snapshot, error)`; `Resume(ctx context.Context, sessionID domain.SessionID, expectedVersion int64, maxRounds *int) (Snapshot, error)`; `Recover(ctx context.Context) error`; `Run(ctx context.Context)`; `Close(ctx context.Context) error`.

- [ ] Napisz test restartu na zamkniętej i ponownie otwartej bazie dla reserved/running tests, fix i review, także awarii tuż po wykonaniu skutku ubocznego. Licznik rzeczywistych startów tej samej operacji wynosi najwyżej 1.
- [ ] Dodaj testy podwójnego zdarzenia i dwóch jednoczesnych `Evaluate`, zatrzymania na każdym etapie, spóźnionego wyniku po stop, queued fix, zmiany generacji i utraty watcherów. Potwierdź oczekiwane błędy.
- [ ] Utrwal stop przed anulowaniem. Nie przechodź do stopped ani nie zwalniaj rezerwacji pracy, dopóki nie potwierdzono końca testów/agenta. Błąd przerwania zapisuj bez pozornego sukcesu.
- [ ] Przy starcie demona odczytaj istniejące operacje. Dla potwierdzonego obrotu/review rekoncyliuj istniejące ID; dla niepotwierdzonego dostarczenia lub procesu ustaw blokadę. Nie wysyłaj ponownie rezerwacji o nieznanym wyniku. Nie przenoś historycznego zatwierdzenia do nowej epoki obserwacji.
- [ ] Wznowienie tworzy nowy attempt dopiero po potwierdzeniu braku poprzedniej aktywności; zachowuje cykl i historię. Gdy `round_limit`, wymagaj jawnego większego `maxRounds`. Wznowienie błędu testów ponawia ten sam cykl; wymagane poprawki po limicie rozpoczynają następny cykl.
- [ ] Podłącz jeden sweep co sekundę do kontekstu demona, po odzyskaniu usług Chat/review. Sweep i CDC mogą inicjować ocenę, lecz stan/CAS rozstrzyga, czy wykonać przejście. Graceful shutdown anuluje procesy i zachowuje fakty; nie oznacza sukcesu.
- [ ] Uruchom `cd backend && go test -race ./internal/localworkflow ./internal/daemon`, z odpowiednim limitem czasu dla pełnych pakietów.

### Task 7: Kontrakt HTTP i typowany klient

**Files:** Create `backend/internal/httpd/controllers/local_workflow.go` and `local_workflow_test.go`; modify `dto.go`, `backend/internal/httpd/api.go`, `apispec/specgen/build.go`; regenerate `apispec/openapi.yaml` and `frontend/src/api/schema.ts`; extend `frontend/src/api/client.ts` using its existing request helpers.

**Interfaces:** `GET /api/v1/sessions/{sessionId}/local-workflow`; `POST .../local-workflow/start` with `{requestId}`; `POST .../local-workflow/stop` with `{expectedVersion}`; `POST .../local-workflow/resume` with `{expectedVersion,maxRounds?}`. Responses contain `Snapshot` through explicit controller DTOs. GET returns an empty workflow/history when none exists, without creating one.

- [ ] Napisz testy httptest: 200/201, powtórzony start, 404 sesji, 409 wersji i aktywnego przebiegu, 422 konfiguracji, 501 brak usługi, malformed/trailing JSON i zachowanie request ID w error envelope.
- [ ] Potwierdź błąd; dodaj DTO do `dto.go`, kontroler jako cienką delegację i rejestrację tras oraz schematów. HTTP nie przyjmuje operation ID, ścieżek logów ani dowodu sukcesu od klienta.
- [ ] GET nie zwraca `Env` ani absolutnej ścieżki workspace/logów. Logi udostępnij przez `GET .../local-workflow/tests/{testId}/logs?stream=stdout|stderr`, po sprawdzeniu właściciela sesji/testu; odpowiedź JSON `{text,truncated}`. Ogranicz pojedynczy odczyt do 256 KiB. Ten route również wpisz do specgen i testów własności.
- [ ] Dodaj metody klienta `getLocalWorkflow`, `startLocalWorkflow`, `stopLocalWorkflow`, `resumeLocalWorkflow`, `getLocalWorkflowTestLog`, zgodnie z wygenerowanymi typami.
- [ ] Uruchom `npm run api`, `cd backend && go test ./internal/httpd/...`, `npm run frontend:typecheck`. Powtórna generacja ma nie zmieniać wygenerowanych plików względem pierwszej.

### Task 8: Sterowanie i historia w istniejącym UI

**Files:** Create `frontend/src/renderer/components/LocalWorkflowPanel.tsx`, `LocalWorkflowPanel.test.tsx`, `frontend/src/renderer/hooks/use-local-workflow.ts`; modify `SessionInspector.tsx`, `ProjectSettingsEditor.tsx`, `ProjectSettingsForm.tsx`, tests, `frontend/src/renderer/lib/event-transport.ts` and tests, existing locale JSON files.

**Interfaces:** `LocalWorkflowPanel({sessionId}: {sessionId: string})` wewnątrz istniejącej sekcji lokalnego review; hook używa metod klienta z zadania 7 i odświeża po CDC `local_workflow_updated`, po reconnect i mutacjach. Limit formularza zapisuje `localWorkflowMaxRounds` jako liczbę całkowitą.

- [ ] Napisz testy widocznych zachowań: zapis/odczyt domyślnego 3 i zmienionego limitu, etap i cykl, błędna komenda z logiem, wymagane uwagi, start/stop/resume, świadome zwiększenie limitu po jego wyczerpaniu, błąd API i konflikt wersji po równoczesnej akcji w innym oknie.
- [ ] Potwierdź oczekiwane błędy w Vitest. Implementuj panel bez przenoszenia logiki przejść do React. Podczas trwającej mutacji blokuj ponowne kliknięcie, zachowuj request ID retry i czytelnie odświeżaj po 409.
- [ ] Uzupełnij etykiety i komunikaty w istniejącym systemie tłumaczeń. Zachowaj opcjonalność sugestii i odróżnienie gotowości od akceptacji użytkownika. Pokaż oznaczenie obciętych logów.
- [ ] Sprawdź CDC i reconnect, zachowanie ręcznego Local review i PR review. UI nie oferuje równoczesnego ręcznego startu review, gdy aktywny etap ma właściciela workflow; backend pozostaje ostateczną kontrolą.
- [ ] Uruchom skupione Vitest, `npm run frontend:typecheck`, `npm run frontend:lint`, następnie pełne `npm --prefix frontend test` i build renderera.

### Task 9: Weryfikacja całej ścieżki i raport OB-4

**Files:** Create `backend/internal/localworkflow/workflow_integration_test.go`, `docs/ob-4-verification.md`; extend HTTP integration tests with real service/store/snapshot dependencies where necessary.

**Interfaces:** Brak nowych publicznych kontraktów. Użyj rzeczywistego SQLite, tymczasowego repo i test runnera; podstaw wyłącznie zewnętrznego Codexa/Claude w deterministycznych testach. Próba z rzeczywistymi agentami jest osobno oznaczona.

- [ ] Najpierw dodaj test akceptacyjny łączący HTTP → koordynator → rzeczywiste komendy → review → poprawki → testy → zatwierdzenie i sprawdź brakujące integracje. Assert: dwa cykle, jedna poprawka, zgodne ID testów i snapshotu, brak mutacji historii Git i brak PR.
- [ ] Dodaj scenariusze akceptacyjne błędu testów, limitu, stop/resume, powtórzonego zdarzenia i restartu procesu z istniejącą bazą. Doprowadź do zaliczenia połączenia usług, nie tylko fake coordinator tests.
- [ ] Uruchom pełne backend `go build ./...`, `go vet ./...`, `go test -race -count=1 -timeout=20m ./...` i golangci-lint **2.13.2** oraz checks z `docs/ci.md` dla zmienionych obszarów. Użyj Node 24 i Go z `go.mod`. Sprawdź wersję Codexa wybieraną przez npm; znany problem starszego klienta opisuje raport OB-3, nie wolno pomijać testu zgodności.
- [ ] Uruchom `npm run ci:test`, `npm run shared:check`, odpowiednie kontrole klientów API i pełny frontend; regeneruj API/sqlc dwa razy, porównując wyniki generacji, zamiast oczekiwać braku diffu względem HEAD zawierającego stare artefakty.
- [ ] Użyj `ao-desktop-dev` do próby rzeczywistego Electrona z tego katalogu i osobnymi danymi pod `~/.ao/dev/ob4-*`. Pokaż pełny przebieg, blokadę i wznowienie. Nie twórz dodatkowego worktree ani nie dotykaj rzeczywistej bazy użytkownika.
- [ ] Przeprowadź niezależny przegląd końcowego diffu zgodnie z wybraną metodą wykonania; rozwiąż uwagi w zakresie OB-4 i ponów dotknięte kontrole. Nie twórz PR ani commita. Osobno odnotuj niedostępne platformy i inne luki w weryfikacji.
- [ ] Zapisz wyniki, polecenia, wersje, dowody kryteriów, zakres podstawionych agentów i wszystkie nierozstrzygnięte ograniczenia w raporcie. Użytkownik otrzymuje zwięzły wynik i link do raportu.

## Samokontrola i przekazanie

Plan pokrywa konfigurację, granice wykonawcy, testy, tożsamość zawartości, review/poprawki, limit, trwałość, stop/resume, UI i odbiór. Pięć dodatkowych klas awarii z Review Focus ma przypisane testy. Ręczne review pozostaje dostępne poza aktywnym właścicielem workflow; Kanban i ekran akceptacji pozostają poza OB-4.

Rekomendowana metoda: **native** — kolejne zadania wykonuje bieżący agent w tej sesji, następnie świeży recenzent sprawdza całą zmianę. Zadania silnie współdzielą kontrakty oraz sekwencję blokad, więc ta metoda ogranicza koszt przekazywania kontekstu i ryzyko równoczesnych zmian tych samych plików.

Status: plan zatwierdzony przez użytkownika; wybrano wykonanie natywne. Implementacja jest prowadzona w bieżącej sesji, z końcowym niezależnym przeglądem i bez commitów oraz worktree. Szczegółowy dziennik decyzji jest w ignorowanym `.superpowers/sdd/2026-10-16-ob-4-local-workflow/progress.md`.
