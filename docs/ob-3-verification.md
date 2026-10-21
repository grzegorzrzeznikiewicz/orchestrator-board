# OB-3 — lokalne review Claude i trwały wynik
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->


Zadanie: [OB-3](https://gamasoftware.atlassian.net/browse/OB-3). Data: 2026-10-16. Gałąź: `feature/OB-3`, utworzona z `origin/main` po scaleniu OB-2. Implementację i próby wykonano bez commitów i push, w głównym katalogu projektu. Po odbiorze użytkownik osobno autoryzował publikację zmian.

## Zachowanie

Local review uruchamia niezależną sesję Claude dla każdej rundy. Zadanie recenzenta zawiera pierwotny prompt wykonawcy, ostatnią dyspozycję użytkownika oraz wskazanie konkretnego snapshotu: manifest, patche HEAD/staged/unstaged i kopię plików, w tym nowych plików nieignorowanych przez Git. Kryteria odbioru są częścią przekazywanej treści zadania; instrukcja nakazuje wynik `error`, jeżeli zadania lub kryteriów nie da się ocenić. Ta ścieżka nie wymaga PR/MR ani publikacji komentarzy.

Wynik `localResult` w wersji 1 wiąże `snapshotId` i `round` z uruchomioną rundą. Zawiera `outcome`, podsumowanie oraz tablicę uwag. Uwagi mają rodzaj `required` albo `suggestion`, ścieżkę względną, dodatni numer linii i uzasadnienie. `changes_requested` wymaga co najmniej jednej wymaganej poprawki; `approved` nie może jej zawierać. Wynik `error` zapisuje stan `failed` bez pozytywnego werdyktu i bez powiadomienia o zatwierdzeniu.

Sam tekst `approved` nie wystarcza do zakończenia lokalnego review. Niekompletny lub sprzeczny wynik powoduje błąd. Niepoprawny JSON, nieznane pola i dodatkowa zawartość po JSON-ie są odrzucane przed zapisem. Lokalne zgłoszenie zawiera dokładnie jeden wynik, aby częściowo zapisany, niejednoznaczny batch nie mógł zatwierdzić zmian. Brak poprawnego wyniku kończy rundę błędem po 15 minutach. Zapis timeoutu jest ponawiany po chwilowym błędzie bazy; spóźniona odpowiedź nie ożywia zakończonej rundy.

Claude otrzymuje ograniczony zestaw narzędzi i ustawienia hooków z katalogu snapshotu, bez dziedziczenia ustawień użytkownika/projektu i zewnętrznych konfiguracji MCP. Hook `PreToolUse` dopuszcza odczyt oraz dokładny, przypisany do wykonawcy kształt polecenia `ao review submit`; blokuje edycje, uruchamianie programów, delegowanie i publikowanie do dostawcy repozytorium. Flagi izolacji trafiają przed separator `--` w rzeczywistym builderze komendy. Każda runda otrzymuje odrębny identyfikator natywnej rozmowy Claude.

Migracja `0200_local_review_result.sql` dodaje numer rundy i JSON wyniku. Zachowuje dotychczasowe werdykty i treść review PR oraz nadaje numery wcześniejszym lokalnym rundom. Nowe rundy są numerowane na podstawie trwałej historii. Kolejność lokalnych wyników zależy od numeru rundy, również gdy czasy zapisu są identyczne.

Interfejs pokazuje numer rundy, identyfikator snapshotu, podsumowanie oraz oddzielnie wymagane poprawki i sugestie z lokalizacjami. Historyczny wynik pozostaje dostępny po restarcie; utrata ciągłości obserwacji unieważnia jego aktualność zgodnie z OB-2. Zatwierdzenie przez recenzenta nie jest akceptacją użytkownika.

## Dowody kryteriów odbioru

| Kryterium | Test lub próba |
| --- | --- |
| Zadanie, kryteria i lokalna zawartość wraz z nowymi plikami | `TestLocalReviewCarriesTaskAndMonotonicRound`, `TestLocalReviewProtocolHTTP`, dotychczasowe testy snapshotów i promptu bez dostawcy PR. |
| Akceptacja, wymagane poprawki i błąd | `TestLocalReviewResultValidation`, `TestLocalReviewProtocolHTTP`, `TestReviewSubmitLocalErrorPreservesStructuredResult`. |
| Uwagi mają lokalizację, uzasadnienie i rodzaj | Walidator domenowy oraz testy `LocalReviewHistory`; brak wymaganych pól i sprzeczność uwag z werdyktem są odrzucane. |
| Brak odpowiedzi i timeout nie oznaczają akceptacji | `TestLocalReviewTimeoutFailsWithoutAnotherRead`, `TestLocalReviewRetriesFailedTimeoutPersistence`; działają bez odpytywania UI. |
| Niedostępny Claude | `TestLocalReviewUnavailableClaudeIsDurableFailure` sprawdza trwały błąd i brak startu recenzenta. |
| Uszkodzony wynik | `TestLocalReviewRejectsUnstructuredApproval`, przypadki protokołu HTTP, `TestLocalReviewHTTPRejectsTrailingData`, `TestLocalReviewDoesNotPartiallyApproveAmbiguousBatch`. |
| Recenzent nie edytuje kodu | `TestLocalReviewerCannotUseProviderOrWriteTools`, `TestLocalReviewerIsolationFlagsPrecedePromptAndNativeIdentityIsPerRun`, `TestLocalReviewerPreToolUseDeniesWritesEvenWithPriorPermission`, `TestLocalReviewerPermissionHookDeniesProviderPublication`. |
| Zmiana plików podczas review blokuje aktualny wynik | `TestLocalReviewHTTPWithoutPRAndStaleResult` obejmuje wyścig przed i po zapisie oraz rzeczywistą edycję pliku; testy strażnika obejmują edycję i przywrócenie treści. |
| Historia pozostaje po restarcie | `TestLocalReviewAndConfigurationSurviveReopen` zamyka i otwiera prawdziwy plik SQLite, porównując pełny wynik, oba rodzaje uwag, rundę i werdykt. |
| Zachowanie historii i trybu PR | `TestMigration0200PreservesPRAndNumbersExistingLocalRounds` oraz istniejące zestawy usługi, silnika, adaptera i CLI review. |

## Próba interfejsu

Uruchomiono rzeczywistą aplikację Electron z tego checkoutu z osobnym `AO_DATA_DIR`, plikiem uruchomienia i profilem Electron pod `~/.ao/dev/ob3-verification-20261016/`. W przygotowanej sesji testowej otwarto zakładkę Reviews, rozwinięto obie rundy i potwierdzono widoczność wymaganej poprawki, sugestii, lokalizacji `login.go:3`, pełnego identyfikatora oraz błędu timeoutu. Były to przygotowane dane do sprawdzenia prezentacji, bez wysyłania zadania do rzeczywistego modelu Claude. Po próbie zatrzymano tę instancję i przywrócono moduł SQLite do ABI Node 24.

## Wyniki kontroli

Kontrole wykonano z Go 1.27.1 i Node 24.19.0. Logi lokalnych poleceń znajdują się w ignorowanym katalogu `.cache/ob3-*.log`.

| Kontrola | Wynik |
| --- | --- |
| Pełny zestaw testów frontendu | 475 plików, 8035 testów zaliczonych, 7 pominiętych. |
| `npm run frontend:typecheck` | Zaliczony. |
| `npm run frontend:lint` | Zaliczony, w tym 6 testów konfiguracji reguł. |
| Produkcyjny build renderera Vite | Zaliczony; pozostaje istniejące ostrzeżenie o wielkości chunków. |
| `go build ./...`, `go vet ./...` | Zaliczone. |
| golangci-lint 2.13.2 | 0 problemów. |
| Testy `-race -timeout=20m` dla silnika review, usługi review, kontrolerów HTTP, store SQLite, CLI i adaptera Claude | Wszystkie 6 pakietów zaliczone. |
| Nowa migracja 0200 z `-race` | Zaliczona. |
| Pełny pakiet migracji SQLite z `-race -timeout=20m` | Zaliczony w 1018,162 s. Wcześniejsze próby z domyślnym limitem 10 minut przekroczyły czas; końcowa próba używa limitu 20 minut z `docs/ci.md`. |
| Ponowne `npm run api` i `npm run sqlc` | 32 wygenerowane pliki bez zmian względem poprzedniej generacji. |
| `npm run ci:test` | 67 testów zaliczonych. |
| Pełny zestaw backendu i golangci-lint w kontrolnym `npm run lint` z Codex 0.161.0 | Zaliczone bez wyłączania kontroli zgodności; analizator zgłosił `0 issues`. Sposób wyboru klienta opisano poniżej. |

## Granice weryfikacji

Pierwsza faza testów zastępowała zewnętrzny proces agenta, zachowując rzeczywiste HTTP, usługę, snapshot i bazę danych. Opisana poniżej próba uzupełnia OB-3 o uwierzytelnionego recenzenta Claude. Nie oznacza wykonania całości szerszego zadania OB-7. Niezależny przegląd kodu objął walidację wejścia, izolację Claude, wyścigi i timeout; po poprawkach nie zgłosił dalszych blokujących problemów.

## Próba z prawdziwym Claude — uzupełnienie z 2026-10-16

Po akceptacji użytkownika przeprowadzono próbę w dozwolonym prywatnym projekcie `budget-planner-app`, w utworzonym wyłącznie do tego celu katalogu `tmp/ob3-live-review-20261016`. Katalog miał własny, pusty Git bez commitów i jeden nowy plik `budget-limit.js`. Istniejących zmian aplikacji nie włączano do snapshotów. Nie otwierano ani nie modyfikowano plików projektów Satisfly. Jawne ścieżki odczytu w transkryptach recenzenta sprawdzono: wszystkie prowadziły do izolowanego katalogu tej próby pod `~/.ao/dev/ob3-live-20261016/`.

Zbudowano binarkę z bieżącego kodu OB-3 i uruchomiono prawdziwy daemon na `127.0.0.1:3093`, z osobnym `AO_DATA_DIR` i `AO_RUN_FILE`. Wyniki nie były seedowane. Utworzono jedynie bezczynnego wykonawcę z zadaniem i kryteriami; recenzenta uruchamiała produkcyjna ścieżka `ao review trigger`, adapter Claude Code, natywny terminal PTY i hooki. Claude Code 2.1.301 używał zalogowanego konta, modelu Opus 5.5 oraz niezmienionych ograniczeń odczytu i publikowania. Sesja odbioru: `ob3-live-budget-4`.

Zadanie określało funkcję `canSpend(balanceCents, expenseCents)`: wyłącznie całkowite, nieujemne wartości liczbowe; wydatek nie większy od salda; dopuszczone wydanie dokładnego salda i zerowy wydatek; odrzucane liczby ułamkowe, wartości ujemne, NaN, Infinity i typy inne niż liczba.

| Runda | Próba i rzeczywisty wynik |
| --- | --- |
| 1 | Celowo błędne `expenseCents < balanceCents`. Claude zgłosił `changes_requested`, dwie wymagane poprawki przy `budget-limit.js:2` i konkretne przykłady niespełnionych kryteriów. Daemon zapisał wynik przez HTTP 200. Źródło pozostało niezmienione. |
| 2 | Przygotowanie kolejnej rundy zakończyło się konfliktem obserwacji workspace. Zapisano `failed` bez werdyktu; nie uznano próby za wykonane review. Dokładnej przyczyny zdarzenia obserwatora nie ustalono. |
| 3 | Po dodaniu `Number.isInteger`, kontroli nieujemności i `<=` Claude zwrócił `approved` z jedną opcjonalną sugestią dotyczącą `Number.MAX_SAFE_INTEGER`. Wynik zapisano przez HTTP 200. Osobno sprawdzono 17 przykładów kryteriów na poprawionej funkcji. |
| 4 | Podczas `running` dopisano komentarz do źródła. AO unieważniło rundę. Claude próbował wysłać rzeczywisty wynik `approved` dla starego snapshotu; daemon odrzucił zgłoszenie HTTP 409, a werdykt w bazie pozostał pusty. Przywrócenie poprzednich bajtów nie przywróciło ważności. Po zebraniu dowodu rundę anulowano. |

Po restarcie daemona porównano pełne trwałe dane wszystkich czterech rund: identyfikatory, numery, snapshoty, statusy, werdykty, treść, JSON wyników, uwagi i unieważnienia. Były identyczne. Historyczne zatwierdzenie pozostało dostępne jako nieaktualne. Hashe plików we wszystkich czterech snapshotach nadal zgadzały się z manifestami.

Zatrzymano procesy próby i usunięto wyłącznie utworzony katalog testowy. Hashe wszystkich 483 istniejących wcześniej plików projektu i wynik `git status --porcelain=v1 -z` były identyczne przed i po próbie. Dowody lokalne: `.cache/ob3-live-scenario1-result.json`, `scenario2-result.json`, `scenario3-result.json`, `scenario3-real-submissions.json`, `before-restart.json`, `after-restart.json` (każda nazwa z prefiksem `ob3-live-`); zachowana baza i manifesty pod `~/.ao/dev/ob3-live-20261016/`.

### Rozpoznane problemy przygotowania środowiska

Pierwsze uruchomienie pomocniczej binarki nazwanej `ob3-live-ao` uruchomiło awaryjne tworzenie shima `ao`. Ta ścieżka skróciła `PATH` do katalogów shima i Claude, usuwając `/usr/bin`; `claude auth status` z takim PATH zwracał brak logowania, a z przywróconym PATH poprawne `claude.ai`. Właściwe próby wykonano z tą samą binarką pod standardową nazwą `ao`, bez osłabiania izolacji ani kopiowania poświadczeń. Nie poprawiano tej pobocznej ścieżki w ramach próby.

Wstępna konfiguracja pomocniczego wykonawcy przez stary runtime tmux ujawniła także odmowę usuwania nieistniejącej sesji (`no such session: =review-…:`). Właściwe próby używały bezczynnego wykonawcy Chat i produkcyjnego natywnego PTY dla Claude. Ścieżka legacy tmux nie jest objęta pozytywnym wynikiem tej próby.

### Zgodność Codex i pełny lint

Ustalono przyczynę wcześniejszego błędu `externalAgentConfig/import/recordHistory`: bezpośrednio z terminala wybierany jest `/opt/homebrew/bin/codex` 0.161.0, natomiast npm dodaje katalogi nadrzędnych `node_modules/.bin` i wybiera `/Users/grzegorzrzeznikiewicz/node_modules/.bin/codex` 0.145.0. Adapter wymaga co najmniej 0.146.0. Powtórzenie testu zgodności i całego pakietu `codexappserver/...` z 0.161.0 zakończyło się sukcesem. Nie stosowano flagi `-skip` ani wyłączania kontroli zgodności, a plików protokołu ani instalacji użytkownika nie zmieniano.

Zwykły `npm run lint` potwierdził ponownie błąd ze starszym klientem wybieranym przez npm. Kontrolne pełne `npm run lint`, z jednorazowym `npm_config_script_shell` wskazującym `.cache/ob3-live-compatible-shell.sh` i shimem wybierającym 0.161.0: **zaliczone, kod wyjścia 0, analizator: `0 issues`**. Log: `.cache/ob3-live-full-lint-compatible.log`; wcześniejszy domyślny przebieg: `.cache/ob3-live-full-lint.log`. Takie wskazanie wersji nie zmienia reguł testu ani analizatora. Domyślny wybór starszego klienta przez npm pozostaje elementem konfiguracji środowiska użytkownika.

## Dokumentacja użyta do konfiguracji Claude

Format blokującego wyniku hooka i semantykę ustawień sprawdzono w aktualnej dokumentacji przez Context7: [PreToolUse](https://code.claude.com/docs/en/hooks), [ustawienia i ich źródła](https://code.claude.com/docs/en/agent-sdk/configuration). Dostępność flag potwierdzono lokalnym `claude --help` dla Claude Code 2.1.300.
