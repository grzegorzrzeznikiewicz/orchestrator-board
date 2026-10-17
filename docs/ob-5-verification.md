# OB-5 — Kanban i historia lokalnego workflow

Zadanie: [OB-5](https://gamasoftware.atlassian.net/browse/OB-5). Gałąź `feature/OB-5`, baza `c97060c` z main po OB-4. Implementację i odbiór wykonano w rzeczywistym katalogu projektu, bez dodatkowych worktree. Publikację gałęzi i scalenie użytkownik zatwierdził osobno po odbiorze zadania.

## Zachowanie

Projekty Local review mają sześć kolumn: Execution, Tests, Local review, Corrections, Awaiting acceptance i Accepted. Projekty PR zachowują dotychczasowy Kanban. Widok wszystkich projektów rozdziela obie tablice i umożliwia przewijanie także przy małej wysokości okna. API listy projektów udostępnia bezpieczne pole `reviewMode`, bez pełnej konfiguracji i środowiska.

Etap lokalny jest wyliczany przez demon z fazy workflow, ostatniej operacji i aktualności dowodów. Nie jest zapisywany jako status sesji. Gotowość wymaga `ready` i aktualnych dowodów; nieaktualny wynik wraca do Local review. Blokady i zatrzymania pozostają przy etapie wymagającym uwagi, a limit rund jest widoczny przy Corrections. Wiadomość wykonawcy „gotowe” ani istniejący status PR nie przenoszą lokalnej karty do sukcesu.

Karta pokazuje wykonawcę, cykl i limit, wynik ostatniej próby testów bieżącej rundy, liczbę wymaganych uwag i pierwszą uwagę, kod i opis blokady oraz nieaktualność weryfikacji. Otwarcie karty wybiera Reviews także przy pierwszym wejściu do sesji.

Panel grupuje trwałe operacje według rund i prób. Pokazuje czasy, oceniony snapshot, wyniki komend i kody wyjścia, podsumowania review oraz osobno wymagane uwagi i sugestie. Logi stdout/stderr pozostają powiązane z konkretnym testem. Odczyt korzysta z istniejącej kontroli właściciela, ograniczenia rozmiaru i granicy AO data dir. Spóźniona odpowiedź logu nie pojawia się po przejściu do innej sesji lub hosta.

Nie powstał mechanizm akceptacji użytkownika. Kolumna Accepted pozostaje pusta; jej zasilenie trwałą decyzją i akcja akceptacji należą do OB-6. Pozytywny wynik Claude oznacza wyłącznie Awaiting acceptance. Odbiór rzeczywistych modeli należy do OB-7.

## Dowody zachowania

| Wymóg | Weryfikacja |
| --- | --- |
| Etapy i blokady wynikają ze stanu demona | `TestWorkflowBoardStageFollowsEvidence`, testy `LocalWorkflowBoard` dla wszystkich faz, limitu i zatrzymania. |
| Nieaktualne review odbiera gotowość | `TestWorkflowFullCorrectionLoop`, osobny przypadek UI nieaktualnego zatwierdzenia. |
| Wyniki i uwagi na karcie | `LocalWorkflowBoard` — ostatnia próba bieżącego cyklu i wymagana poprawka. |
| Otwarcie karty wybiera Reviews | Test integracji `SessionsBoard` z inicjalizacją stanu sesji po nawigacji; także pierwsze otwarcie świeżej sesji w Electronie. |
| Historia i powiązanie logów | `LocalWorkflowPanel` — dwie rundy, snapshot, wymagane uwagi i sugestie, log pierwszej rundy i ponowne otwarcie panelu. |
| Brak przecieku spóźnionego logu | `LocalWorkflowPanel` — odpowiedź wcześniejszej sesji po zmianie właściciela panelu. |
| Trwałe review po restarcie | `TestLocalWorkflowHTTPRunsTestsCorrectionsAndRealReviewService` zamyka SQLite i usługi, otwiera je ponownie i porównuje oba zapisane review oraz wyniki testów. Brak strażnika daje `current=false` i Local review. |
| Granica HTTP i prywatne dane | `TestLocalWorkflowHTTPContract` sprawdza etap i historię oraz brak prywatnych ścieżek i identyfikatora obserwacji. |
| Tryb PR i tablica mieszana | Pełne zestawy `SessionsBoard`, `SessionsBoard.remote`, `useWorkspaceQuery`; test mieszanej listy Local/PR. |
| Wybór trybu bez pełnej konfiguracji | `TestManager_ListIncludesOnlySummarySafeProjectConfig` i test mapowania listy projektów w `useWorkspaceQuery`. |

## Odbiór Electrona

Uruchomiono bieżący checkout na izolowanych danych `~/.ao/dev/ob5-visual/data`, z osobnym profilem `~/.ao/dev/ob5-visual/electron` i `running.json` w tym samym katalogu nadrzędnym. Demon: `127.0.0.1:3005`; renderer: `localhost:5173`. Wskazano istniejący tmux z instalacji aplikacji. Nie używano rzeczywistej bazy projektów użytkownika.

Kontrolowane rekordy SQLite zawierały dwie rundy: pozytywne testy i review wymagające poprawki w rundzie 1, a następnie test zakończony kodem 7 w rundzie 2. Nie uruchamiano modeli. W natywnym oknie potwierdzono sześć kolumn, kartę w Tests, cykl 2/3, wynik 0/1, wymaganą uwagę i blokadę `tests_failed`. Panel pokazał obie rundy, wymagane i opcjonalne uwagi oraz stdout rundy 2. Świeża karta po poprawce inicjalizacji otworzyła od razu Reviews.

Pierwszy szkic fixture Chat nie miał kontrolera i został przez istniejącą obsługę sesji zarchiwizowany. Do odbioru karty użyto fixture TUI bez uruchamiania agenta. Pierwszy start aplikacji wymagał jawnego wskazania istniejącego tmux; po restarcie i odświeżeniu renderera wykonano opisany odbiór.

## Kontrole

Potwierdzone: pełne race suites `localworkflow`, `service/project` i `httpd/...`; dodatkowy test HTTP historii po ponownym otwarciu SQLite; backend build i vet; golangci-lint 2.13.2: zero uwag; regeneracja API bez dryfu; ESLint i 6 testów konfiguracji lint; 67 testów kontraktu CI. Skupiony przebieg panelu, kart i ustawień języka: 65 testów passed. Niezależny reviewer zatwierdził zmiany po poprawce przewijania i ponownym sprawdzeniu inicjalizacji panelu.

Pełny frontend z Node 24.19.0 i `vitest run --maxWorkers=4`: **477 plików passed, 8054 testy passed, 7 istniejących skipped** (190 s). Po poprawieniu typu fixture ponownie przeszło 10 testów kart. Pełny TypeScript (`tsc --noEmit --incremental`, cache poza źródłami, limit heap procesu 8 GiB) zakończył się z exit 0. Wcześniejsza kontrola wskazała niepoprawny fixture sesji; zastąpiono go kompletnym rekordem z obsługiwanym statusem i ponowiono kontrolę. Pierwszy pełny frontend uruchomiony z błędnego cwd ujawnił błędy odczytu względnych fixtures; poprawiono wywołanie. Uzupełniono wymagane formy liczby mnogiej. Osobny przebieg pod dużym obciążeniem zgłosił timeout testu ustawień języka; ten sam pełny plik przeszedł następnie w skupionym przebiegu. Końcowy pełny frontend ma ograniczoną współbieżność.

Na etapie implementacji nie wykonywano zdalnego CI ani pakowania wydania. Logi i pomocniczy seed są w ignorowanych katalogach `.cache/ob5-*` oraz `backend/.cache/ob5-desktop-seed`.

Testową instancję Electron, Forge i demona zakończono po sprawdzeniu ich PID-ów; port 3005 nie ma nasłuchującego procesu.

## Weryfikacja przed publikacją — 2026-10-17

Uruchomiono komplet wybranych lokalnie kontroli CI z Go 1.27.1 i Node 24.19.0. Pełny backend z `-race -count=1 -timeout=20m` przeszedł w podziale identycznym jak CI: dwa shardy core, dwa Chat, cztery SQLite oraz session manager i store. Przeszły także build, vet, golangci-lint 2.13.2 (zero uwag), kontrola formatowania wszystkich 1906 plików Go ze źródeł repozytorium i regeneracja SQL/API bez dryfu.

Ponownie przeszedł pełny frontend: 477 plików, 8054 testy passed i 7 skipped; pełny TypeScript, typy E2E, ESLint i testy reguł; 83 testy renderer smoke; build dokumentacji; mobile: typy i 214 plików / 1908 testów; cloud-client: 29 testów, typy, generacja i build; product-ui: 160 testów, typy i build. Oba pakiety przeszły `npm pack --dry-run` z katalogu pakietu. Pierwsze wywołanie dry-run z błędnym cwd poprawiono bez zmian źródeł. Node 22.23.2 przeszedł sprawdzenie zgodności lockfile przez `npm ci --ignore-scripts --dry-run --no-audit --no-fund`.

Natywny CLI E2E na macOS i świeża instalacja CLI w kontenerze Linux przeszły. Testy konfiguracji helpera macOS, kompilacja x64 i arm64, kontrola `lipo` oraz natywne testy stanu Swift przeszły. Kontrole kontraktu CI, testy skryptu gate i actionlint przeszły.

Lokalna integracja `test:browser-postcondition-integration` dwukrotnie zatrzymała się na `capturePage()` z komunikatem `Current display surface not available for capture`. Kontrolowane wybudzenie wyświetlacza nie zmieniło wyniku. Test i kod procesu main nie są częścią zmian OB-5. Nie zaliczono tej kontroli lokalnie; wymagane jest jej potwierdzenie na Linuxie w CI przed scaleniem. Natywne kontrole Windows, CLI na pozostałych OS oraz uruchomienie helpera na runnerze macOS Intel również wymagają zdalnego CI. Nie wykonywano publikacji wydania ani produkcyjnego deploymentu. Logi tego przebiegu: `.cache/ob5-publish/`.
