# OB-4 — automatyczna pętla lokalnego workflow

Zadanie: [OB-4](https://gamasoftware.atlassian.net/browse/OB-4). Gałąź `feature/OB-4`, baza `c29627b` z main po scaleniu OB-3. Praca odbywa się w rzeczywistym katalogu projektu, bez dodatkowych worktree, commitów i push.

Status raportu: implementacja, poprawki po niezależnym przeglądzie i dostępne kontrole kodu zakończone. Po odblokowaniu Maca zaliczono także odbiór panelu/ustawień w rzeczywistym Electronie na kontrolowanych danych oraz integrację przechwytywania obrazu. Pozostałe luki platformowe i odbiór rzeczywistych modeli są wymienione poniżej.

## Zaimplementowane zachowanie

Użytkownik uruchamia workflow dla istniejącej sesji Codex. Demon prowadzi testy, niezależne lokalne review Claude, wymagane poprawki i kolejne testy/review. Domyślny limit to 3 cykle; ustawienie jest zapisywane w konfiguracji projektu i w przebiegu. Sugestie pozostają opcjonalne. Pusta konfiguracja testów blokuje automatyzację, zachowując ręczne Local review.

Migracja 0201 dodaje trwałe przebiegi, operacje i wyniki komend. Rezerwacje oraz przejścia używają wersjonowania i transakcji. Zlecenie review zostaje trwale powiązane z operacją przed uruchomieniem recenzenta. Wiadomości poprawek mają trwały identyfikator, a intake Chat odrzuca wiadomość przeznaczoną dla poprzedniej generacji kontrolera.

Testy są wykonywane kolejno w katalogu sesji. Każda komenda ma limit 20 minut, wynik, kod wyjścia i oddzielne logi stdout/stderr. Log ma limit 10 MiB na strumień; HTTP odczytuje najwyżej 256 KiB i oznacza ucięcie. Prywatny host testów zachowuje oznaczenie startu; brak wyniku po awarii nie pozwala uruchomić tej samej operacji ponownie. Zatrzymanie jest utrwalane przed anulowaniem; stary wynik nie wznawia procesu.

Przed testami powstaje manifest i strażnik plików, utrzymywany przez testy i review. Review musi dotyczyć przetestowanej zawartości. Gotowość wymaga pełnych pozytywnych wyników wszystkich komend oraz aktualnego strukturalnego zatwierdzenia tego samego snapshotu. Zmiana plików albo utrata obserwacji po restarcie odbiera aktualność. Ponowna próba po świadomym wznowieniu otrzymuje nowe identyfikatory i zachowuje licznik oraz historię.

Workflow nie wykonuje poleceń commit/push ani nie tworzy PR/MR. Prompt wymaganych poprawek nakazuje pozostawić zmiany bez commitów. Ustawienia i istniejąca sekcja Reviews zawierają limit, etap, cykl, blokadę, wymagane uwagi i wyniki komend z logami. Sterowanie frontendowe korzysta z API demona.

## Dowody zachowania

| Wymóg | Test |
| --- | --- |
| Pełny cykl z poprawką i ponowną weryfikacją | `TestWorkflowFullCorrectionLoop`; `TestLocalWorkflowHTTPRunsTestsCorrectionsAndRealReviewService`. |
| Limit i jawne zwiększenie | `TestWorkflowLimitTestFailureAndMissingConfiguration`; `TestWorkflowLimitRequiresExplicitIncrease`; testy `LocalWorkflowPanel`. |
| Testy muszą zakończyć się przed review | Scenariusz `tests_failed` oraz `TestWorkflowRejectsIncompleteTestEvidence`. |
| Snapshot review odpowiada testom | `TestWorkflowReviewBindsTestedSnapshotAndLaunchIdentity`; kontrole identyfikatorów w pełnej pętli. |
| Aktualność po zmianie plików | `TestWorkflowFullCorrectionLoop` oraz dotychczasowe testy strażnika OB-2/OB-3. |
| Trwałość i restart | `TestLocalWorkflowPersistenceCASAndRestart`; `TestWorkflowRestartAndDuplicateEvaluation` zamyka i otwiera SQLite przed odzyskaniem. |
| Powtórzenia nie dublują skutków | CAS w store, równoległe Evaluate, identyczne request ID i identyczne ID zlecenia review, trwałe oznaczenie uruchomienia komend. |
| Brak nakładających się procesów po awarii hosta | `TestDeadTestHostDoesNotAllowOverlappingRetry` zabija rzeczywisty host przy żyjącym subprocessie; `TestStopPreservesUncertainTestExecution`. |
| Kolejność blokad i anulowanie kolejki Chat | `TestWorkflowStopDoesNotInvertChatSendAdmissionOrder`; `TestWorkflowCancellationSerializesWithSelectedQueuedTurn` używają rzeczywistego kontrolera i SQLite. |
| Zachowanie decyzji użytkownika przed Nudge | `TestWorkflowInputLeasePreservesNudgeSafetyChecks`: prawdziwy input lease, SQLite i guard, stany blocked/waiting_input/exited; osobna próba bezpiecznej wysyłki. |
| Zamrożone kryteria recenzji | `TestWorkflowReviewerKeepsFrozenTaskAfterWorkerRefresh`. |
| Stop i świadome Resume | `TestWorkflowStopAndResumeDoNotResetCycle`; `TestWorkflowResumeRunsNewTestsAndReviewForSameBytes`. |
| Nowy kontroler nie dostaje starej poprawki | `TestWorkflowChangedWorkerDoesNotReceiveFix`; `TestLocalWorkflowFixCannotReachReplacementController`. |
| Ręczne terminal/API nie edytują podczas weryfikacji | `TestWorkflowVerificationBlocksDirectTerminalInput`; `TestLocalWorkflowBlocksWorkerDuringVerification`; intake Chat w transakcji. |
| Przerwane wykonanie nie oznacza sukcesu | `TestWorkerInitialFailureIsNotSuccessfulIdle`; timeout/anulowanie runnera. |
| Brak konfiguracji lub dostępności | `TestWorkflowUnavailableExecutorHasDurableBlock`; scenariusz braku testów i błędów review. |
| Logi i prywatność | `TestWorkflowLogIsBoundedOwnedAndCannotEscapeDataRoot` sprawdza 10 MiB, odczyt 256 KiB, właściciela i symlink poza data dir; `TestLogOpenFailurePreventsCommandExecution`. |
| Bezpieczeństwo tożsamości procesu | `TestCancellationNeverSignalsARecordedForeignPID`: anulowanie pisze żądanie do hosta, nie sygnalizuje obcego procesu o zapisanym PID. |
| Granica HTTP | `TestLocalWorkflowHTTPContract`: strict JSON, błędy 400/404/409/422/501, brak prywatnego środowiska i ścieżek w DTO. |
| Historia review PR pozostaje zachowana | `TestMigration0201PreservesReviewsAndEmitsWorkflowCDC` i istniejące zestawy review. |

W próbie HTTP rzeczywiste są HTTP, koordynator, SQLite, subprocess testów, manifesty i strażniki oraz silnik/usługa review i przyjmowanie wyników przez `/reviews/submit`. Podstawione są wyłącznie zewnętrzne modele: recenzent otrzymuje kontrolowany wynik, a wykonawca wykonuje kontrolowaną zmianę pliku. Ta próba nie jest próbą z uwierzytelnionym Codexem i Claude; szerszy odbiór rzeczywistych agentów pozostaje w OB-7.

## Istotne decyzje i ograniczenia

- Automatyczne poprawki korzystają z trwałego zakończenia obrotu **Codex Chat**. Dla TUI samo semantyczne przyjęcie wiadomości i `idle` nie dowodzą udanego zakończenia. Gdy brakuje takiego dowodu, proces blokuje się z informacją o przejściu do Chat, zamiast wysyłać lub zatwierdzać niepewną poprawkę.
- Po utracie strażnika przy restarcie potrzebne jest świadome wznowienie i świeże testy/review. Aktywne albo niepewne wcześniejsze procesy blokują wznowienie.
- Nowe triggery używają istniejącego CDC `session_updated` z `workflowId`; poszerzanie zamkniętego enumu `change_log` wymagałoby przebudowy wszystkich triggerów. Klienci odświeżają stan po CDC oraz reconnect.
- Logi i dane operacji pozostają pod AO data dir, poza repozytorium; HTTP sprawdza właściciela testu i ogranicza otwieranie plików przez `os.OpenRoot`.
- Komendy w nowych scenariuszach subprocessów są POSIX. Na Windows te konkretne fixtures są jawnie pominięte; kompilacja Windows i wykonanie na natywnym Windows są odrębnymi kontrolami.

## Środowisko i kontrole

Go 1.27.1 z lokalnego toolchaina, Node 24.19.0 z runtime Codex. Domyślny npm wybiera Node 25.6.0; nawet `/opt/homebrew/opt/node@24` wskazuje na Node 25.6.0. Domyślna próba pełnego frontendu zgłosiła niezgodność ABI modułu SQLite (137 vs 141). Kontrole Node 24 używają wyłącznie taskowego shima i `npm_config_script_shell` w `.cache/ob4-*`, bez zmiany instalacji użytkownika.

Potwierdzone kontrole:

- Pełny frontend: **476 plików, 8040 testów passed, 7 skipped** (Node 24). Pominięcia pochodzą z istniejącego zestawu.
- Mobile, odbiorca typów API: **214 plików, 1908 testów passed**, TypeScript passed.
- Wszystkie pakiety backendu poza SQLite i pierwszą próbą session_manager przeszły pełne race tests. Pierwsza próba session_manager nie miała tmux na PATH; po wskazaniu istniejącego runtime pełny zestaw session_manager przeszedł (310 s).
- Po poprawkach ponownie przeszły pełne race suites localworkflow/review/httpd/daemon/session_manager/sessionguard/terminal. Po dodatkowej korekcie granicy input ponownie przeszły localworkflow/sessionguard/terminal; ostatni pełny session_manager z testem produkcyjnego SQLite także przeszedł (302,833 s; `.cache/ob4-session-manager-complete.log`).
- Pełny SQLite bez shardów przekroczył 20 minut w kolejnym teście migracji (sam ostatni test działał 2 s). Wszystkie 110 nazw testów przeszło następnie w czterech shardach identycznych z CI: 343,431 s; 332,284 s; 387,829 s; 364,730 s (każdy exit 0).
- Backend build/vet passed; Windows amd64 cross-build passed. Wykonanie na natywnym Windows/Linux pozostaje kontrolą CI.
- Golangci-lint 2.13.2: zero uwag. Ponowienie po dodaniu integracyjnych scenariuszy Chat również: zero uwag.
- Frontend TypeScript, E2E TypeScript, ESLint + 6 testów reguł passed. Renderer smoke: **83 passed**. Dokumentacja: build passed.
- Native CLI E2E macOS passed (38 s); świeża instalacja w kontenerze Linux: `fresh-install check: OK`.
- 13 testów konfiguracji/bezpieczeństwa pakowania natywnego helpera passed. Po Electronie potwierdzono ponowne ładowanie SQLite addon w Node 24.
- Generatory API/sqlc: dwa kolejne uruchomienia dały identyczną zawartość 33 plików; `git diff --check` passed.
- Testy selektora i kontraktu CI: 67 passed. Actionlint 1.7.12 passed. Shared cloud-client/product-ui check passed.

Niezależny końcowy reviewer zgłosił 4 uwagi P1 i 1 P2. Wszystkie potwierdzono testami RED przed poprawką, następnie GREEN. Obejmowały niepewny koniec subprocessu, kolejność blokad Stop, anulowanie kolejki bez sendMu, zmianę stanu przed safety check oraz utratę zamrożonego promptu. Dodatkowe testy z produkcyjnym Chat kontrolują wskazane przeploty wykonania. Reviewer nie wydawał ponownej oceny po poprawkach; jeden final review i jeden fix pass odpowiadają zaakceptowanemu trybowi native.

## Odbiór Electrona po odblokowaniu Maca — 2026-10-17

Wcześniejszą próbę blokował ekran blokady. Po odblokowaniu ponowiono `npm --prefix frontend run test:browser-postcondition-integration`: **exit 0, browser action postcondition integration passed** (`.cache/ob4-browser-postcondition-unlocked.log`).

Rzeczywisty Electron z bieżącego checkoutu, przebudowanym demonem i działającym preloadem uruchomiono na porcie 3004. Dane, running.json i profil Electron znajdowały się w osobnym `~/.ao/dev/ob4-verification`; rzeczywista baza użytkownika nie była używana. Projekty i sesje odczytywano przez rzeczywiste API. Pomocniczy seed w ignorowanym `backend/.cache/ob4-desktop-seed` utworzył jedynie fixture projektu i sesji; nie uruchamiał modeli.

Ręcznie w natywnym oknie potwierdzono:

- `Start workflow` → `Waiting for executor` → `Running tests`; ręczny `Run local review` był wyłączony podczas weryfikacji.
- Kontrolowana komenda kończyła się kodem 7: `Workflow blocked`, `Cycle 1 of 3`, wynik `Failed (7)`, prawidłowy odczyt stdout `OB-4 desktop verification` i stderr `controlled failure`.
- `Stop` → `Stopped` → `Resume`: zachowany cykl 1/3, nowa próba komendy, zachowana historia obu wyników.
- Osobna 30-sekundowa komenda została zatrzymana przed końcem: `Stopping` → `Stopped`, wynik `Interrupted (-1)`. API potwierdziło trwałe `phase=stopped`, `cycle=1`, `maxRounds=3` i wynik `interrupted` także po restarcie demona.
- W ustawieniach Local review zmieniono limit 3 → 4; API potwierdziło zapis 4. Następnie przywrócono i potwierdzono 3. Lokalny zrzut dowodu: `.cache/ob4-desktop-limit.png`.

Pierwszy szkic fixture nie zawierał roli orchestrator, więc formularz poprawnie odmówił zapisu ustawień. Uzupełniono fixture i ponownie uruchomiono renderer z pełną konfiguracją; zapis limitu przeszedł. To nie wymagało zmiany kodu produktu. Sesje fixture nie miały prawdziwego terminala agenta; nie oceniano na ich podstawie startu modelu ani terminala. Pełna pętla modeli pozostaje odbiorem OB-7, a jej integracja usług jest pokryta opisaną wcześniej próbą HTTP z kontrolowanymi modelami.

Testowe Electron/Forge i demon zakończono po zweryfikowanych PID-ach. Po przebudowie modułu SQLite dla Electron przywrócono ABI Node 24. Nie dodano zmian implementacji, commitów ani push w trakcie odbioru.

Końcowe `go build ./...`, `go vet ./...`, cross-build Windows amd64 i lint nowego zestawu log/process ponownie przeszły po ostatniej zmianie źródeł. Wszystkie cztery shardy SQLite zakończyły się sukcesem. Testowe procesy Electron/daemon/Forge oraz uruchomione zestawy testów zakończono; nie pozostawiono aktywnej instancji aplikacji z tego checkoutu.

Pozostałe luki: natywne pełne zestawy backendu/CLI na Windows i Linux, natywne kompilacje/pakowanie helpera dla obu architektur macOS oraz zdalne kontrole CI/Gitleaks nie zostały zaliczone lokalnie. CLI w kontenerze Linux zostało sprawdzone. Na etapie lokalnego odbioru nie uruchamiano zdalnego CI, ponieważ obowiązywał zakaz commitów/push. Po późniejszej zgodzie użytkownika publikacja PR wymaga kontroli zdalnego CI przed scaleniem. Rzeczywiste modele i szerszy hosted E2E pozostają odbiorem OB-7. Żadnej z tych kontroli nie oznaczono jako lokalnie zaliczonej.

Logi poleceń: ignorowane `.cache/ob4-*.log`. Oryginalna próba przechwytywania na zablokowanym ekranie została zastąpiona udanym ponowieniem po odblokowaniu. Oryginalne nieudane próby i log mieszany przerwanej nadmiarowej próby session_manager pozostają zachowane; o wyniku końcowym decydują jawne ponowienia opisane powyżej. Baza implementacji: `c29627b`, gałąź `feature/OB-4`. Implementację i odbiór pozostawiono początkowo bez commitów i push. Następnie użytkownik jawnie autoryzował commit, push, PR i scalenie do main; publikacja używa istniejących wyników, dodatkowego sprawdzenia przed commitem i wymaganych kontroli zdalnego CI.
