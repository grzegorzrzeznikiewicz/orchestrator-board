# OB-7 — regresja lokalnego workflow
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->


Status na 17.10.2026: lokalne testy, analiza statyczna, odbiór manualny i niezależny review zaliczone; zdalny CI wymaga sprawdzenia po publikacji commita. Baza main `cc3bdffd63cf74291f8ad3bb550cf8beb35ea5a0`, branch `feature/OB-7`.

## Kryteria odbioru

Źródło: Jira OB-7, odczyt 17.10.2026. Testy integracyjne i end-to-end całego lokalnego procesu oraz próby Codex + Claude na Macu mają sprawdzić przejścia stanów, trwałość, powiązanie wyników z plikami i dotychczasowe review PR. Wymagany udokumentowany scenariusz wykonanie → testy → review z uwagami → poprawki → pozytywne testy/review → ręczna akceptacja. Macierz obejmuje nowe i niezacommitowane pliki, zmianę po review/akceptacji, błąd testów, timeout, niepoprawny wynik, limit rund i restart. Raport zawiera wyniki, logi i ograniczenia; nierozwiązany błąd nie jest sukcesem.

## Sposób weryfikacji

Testy Go używają rzeczywistych plików, komend testowych, SQLite i usług review; model oraz potwierdzenie desktopowe są kontrolowanymi sterownikami. Vitest sprawdza granice main/renderer. Renderer smoke używa atrap bridge i nie jest pełnym testem demona. Osobna próba rzeczywistego Electrona, Codexa i Claude na macOS potwierdziła opisany poniżej proces użytkowy w granicach przygotowanej próbki.

## Projekt próbny

Przykład `sum(a,b)` ma przyjmować wyłącznie skończone liczby i odrzucać pozostałe wartości przez `TypeError`. Testy: `node --test sum.test.js`; co najmniej dodatnie, ujemne i zerowe argumenty oraz odrzucanie tekstu, NaN i nieskończoności. Pliki mają pozostać lokalne; workflow nie wykonuje commit, push ani PR w projekcie próbnym.

## Wyniki

Poniższe wyniki pochodzą z uruchomień na finalnych źródłach OB-7. Końcowy zdalny CI oraz scalony commit są odnotowywane w PR, Jira i Confluence po publikacji.

| Granica | Próba | Wynik |
| --- | --- | --- |
| HTTP → proces testowy → SQLite | `TestLocalWorkflowHTTPFailedTestsRemainFailedAfterReopen`: exit 7 i rzeczywisty proces `sleep 10` z budżetem 300 ms | Zaliczone. Status, exit code i log sprawdzone przed ponownym otwarciem; po otwarciu sprawdzone status/tożsamość, brak review/poprawki/decyzji i fałszywej aktualności/akceptacji. |
| HTTP → walidacja review → workflow | `TestLocalWorkflowHTTPInvalidReviewerCannotAuthorizeAcceptance`: brak wyniku, obcy snapshot, nieznana wersja, required finding z approved, jawny error modelu | Zaliczone. Odrzucenie lub zapis błędu, blokada, brak akceptacji; stary wynik nie daje się przepisać. Resume wymaga nowych testów i nowego review. |
| Nowy plik po akceptacji | `TestLocalWorkflowHTTPChangedNewFilesInvalidateAcceptedDecision` | Zaliczone. Aktualność znika, decyzja zachowuje ID; retry nie akceptuje nowych treści i nie zapisuje drugiej decyzji. HEAD pozostaje unborn. |
| Main → prywatne potwierdzenie → retry | Test hosta desktopowego z utraconą odpowiedzią po zapisie | Zaliczone z kontrolowanym callbackiem demona. Stały requestId, nowa prywatna intencja, tylko jeden dialog. Nie jest to pełny test rzeczywistego transportu sieciowego. |
| Spóźniona decyzja i zmiana hosta | Panel z tym samym sessionId, zmienionym hostId i późnym sukcesem/błędem | Zaliczone. Komentarz i błąd nie przechodzą na nowy host; nie pojawia się akceptacja, przyciski zdalnego odbioru pozostają zablokowane. |

Nowa macierz HTTP z `-race -count=1`: 76,688 s, wynik PASS. Skupione testy main/panel: 2 pliki, 24 testy, PASS. Są to nowe testy istniejącego poprawnego zachowania; nie przedstawiamy ich jako naprawy odtworzonego błędu.

## Rzeczywisty Electron, Codex i Claude

Checkout uruchomiono na `feature/OB-7`, renderer `localhost:5173`, demon `127.0.0.1:3002`. Dane, profil i plik uruchomienia są osobne: `~/.ao/dev/ob7/{data,electron,running.json}`. Test nie dotyka bazy projektów użytkownika.

Normalny spawn utworzył sesję Codex Chat `standalone-1`. Codex wykonał `git init -b main`, zapisał `sum.js` i `sum.test.js`, uruchomił 4 testy z wynikiem PASS. HEAD pozostał unborn; pliki były nieśledzone. Aby spełnić zakaz dodatkowych worktree, testowa konfiguracja projektu `single_repo`, rzeczywisty branch i powiązania sesji/rozmowy zostały przygotowane w izolowanej bazie. Ścieżki projektu, workspace i workspace repo wskazują wyłącznie katalog tej próbki. Nie dodano `session_worktrees`, nie zmieniano identyfikatorów providera, generacji, wiadomości, wyników modeli ani decyzji. Ta próba sprawdza odzyskanie żywego Chat/workflow; nie jest próbą zwykłego dodawania projektu, spawnu projektu ani przywracania Git worktree.

### Review z wymaganymi uwagami i poprawka

Zachowano pierwotne poprawne pliki. Następnie jawnie zasiano regresję `return Number(a) + Number(b)` oraz pojedynczy test dodatnich argumentów. Test nadal przeszedł; nie był dowodem spełnienia kryteriów zadania. Po uruchomieniu workflow w rzeczywistym oknie Claude ocenił snapshot nowych plików bez HEAD i zgłosił dwie wymagane poprawki: odrzucanie błędnych typów/NaN/nieskończoności i pełniejsze testy. Wyniku review nie wstrzykiwano przez API.

Workflow dostarczył poprawkę rzeczywistemu Codexowi. Codex przywrócił walidację i dodał przypadki testowe. Nowy cykl 2 miał nowy snapshot, pozytywne testy i rzeczywiste `approved` Claude. Natywny dialog Electrona zapisany na zrzucie został potwierdzony: API zwróciło `accepted=true`, `current=true`, jedną decyzję powiązaną z dowodami cyklu 2.

### Restart żywej sesji

Zapisano stan i tożsamości, wywołano zwykłe lokalne `/shutdown`, a następnie widoczny przycisk Restart daemon. PID demona zmienił się z 67776 na 94698. Ten sam `standalone-1` pozostał niezakończony i idle; API pokazało `chatProviderPreserved=true`. Identyfikator rozmowy providera pozostał ten sam, generacja kontrolera zmieniła się. Historia zachowała decyzję, a `accepted/current=false` i `restart` wymagały nowej weryfikacji. Rzeczywisty panel pokazał Historical acceptance i Resume workflow. Jawne Resume wykonało nowe testy oraz trzecie review Claude, które zakończyło się approved. Nie odtwarzano ani nie powtarzano poprzedniego zadania.

### Odesłanie, limit i błąd testów

Natywnie odesłano komentarz z prośbą o osobno nazwany test `sum(-7,7)===0`. Codex faktycznie dodał ten test; cykl 3 zakończył nowe testy i pozytywne review. Kolejny komentarz o nazwanym teście argumentów dziesiętnych zapisano przy 3/3. Panel pokazał `round_limit` i wyłączone Resume; nie powstała nowa operacja poprawki. Dopiero jawne wprowadzenie limitu 4 i Resume uruchomiły jedną poprawkę. Codex dodał nazwany test `sum(1.25,2.5)===3.75`; cykl 4 zakończył pozytywne testy i rzeczywiste review, a natywny dialog zapisał kolejną akceptację. Komentarz nie zawierał instrukcji czekania, która ograniczyła próbę OB-6.

Po akceptacji celowo zmieniono `sum.js` na zwracanie stałej 12345. API i rzeczywisty panel odebrały aktualność akceptacji (`workspace_changed`), zachowując cztery decyzje. Jawne Resume wykonało rzeczywisty test Node i zapisało `failed`, exit 1, `tests_failed`, bez nowego review i bez dostarczenia poprawki modelowi. Odczytano stdout/stderr przez API. Przywrócenie poprzedniego poprawnego pliku nie przywróciło zgody: kolejne jawne Resume wykonało nowe testy i szóste review Claude. Dopiero nowa decyzja w natywnym dialogu dała końcowe `accepted=true`, `current=true`.

Końcowa historia próby: 7 wykonań komendy testowej (6 sukcesów, 1 celowa porażka), 6 rzeczywistych review Claude (pierwsze changes_requested, kolejne approved), 3 dostarczone poprawki Codexa i 5 decyzji człowieka. HEAD nadal nie istnieje; nie wykonano commita, push, PR ani dodatkowego Git worktree w projekcie próbnym. Negatywna próba jest zachowana jako porażka, a końcowe zaliczenie wskazuje wyłącznie późniejsze świeże dowody.

## Dowody i ograniczenia

Logi, dokładne odpowiedzi HTTP, tożsamości przed/po restarcie i zrzuty tej próby są w ignorowanym workspace planu `.superpowers/sdd/2026-10-17-ob-7-regression/`; po zamknięciu planu zostaną zachowane z manifestem SHA-256 w `.cache/ob7-evidence/`. Końcowy stan maszyny to `manual-final-accepted.json`; `manual-final-verification.json` jest wcześniejszym stanem pośrednim. Zrzuty są obrazami rzeczywistego okna, nie nagraniem wideo. Wyniki OB-6 są opisane osobno w `ob-6-verification.md`.

Niezależny końcowy review: APPROVE, bez Critical/Important. Odłożone drobne rozszerzenia pokrycia: różne dane akceptacji obu hostów w teście przełączenia hosta oraz ponowna asercja exit code/logu po otwarciu SQLite. Obecny test panelu dowodzi izolacji komentarza i późnego sukcesu/błędu przy identycznych niezaakceptowanych danych obu hostów. Test bazy sprawdza diagnostykę przed restartem i tożsamość/status po restarcie.

Po odbiorze zatrzymano wyłącznie testowego demona, host Codexa i Electron/Forge tej próby; dane zachowano. Nie wykonywano procedur porzucających sesję ani porządkowania workspace.

## Pełne lokalne kontrole

Go 1.27.1, Node 24.19.0, golangci-lint 2.13.2; wszystkie 24 grupy w `status.json` zakończyły się kodem 0. Pełny backend wykonano z `-race -count=1`, z rozdzieleniem pakietów i ciężkich testów SQLite/Chat na rozłączne grupy, bez pomijania testów. Native CLI E2E uruchomiono osobno z `-tags e2e`.

| Kontrola | Wynik |
| --- | --- |
| Pełny backend z race | PASS: core (198 testowanych pakietów), SQLite (4 grupy), Chat (2 grupy), session manager i store |
| Dotychczasowe review PR | PASS pakietów review, service/review, reviewer/claudecode, controllers, integration i CLI; nie jest to nowa próba żywego zdalnego PR |
| Go build, vet, golangci-lint | PASS; 0 issues; gofmt czysty dla 2352 źródeł Go |
| Pełny frontend Vitest | 478 plików; 8073 passed, 7 istniejących skipped |
| Typy frontend i E2E, ESLint | PASS; 6 testów reguł lint PASS |
| Renderer smoke | 83 passed; kontrolowany bridge |
| Integracja browser postcondition | PASS |
| Mobile | 214 plików; 1908 passed, typecheck PASS |
| Shared cloud client / product UI | 29 / 160 passed; typy, build i package dry-run PASS |
| CI contract / pod gate / actionlint | 67 / 17 passed; actionlint PASS |
| sqlc i OpenAPI/TS | Regeneracja PASS; 33 hashe bez zmian |
| Świeża instalacja CLI Linux | Build i uruchomienie kontenera PASS |
| Native CLI E2E macOS | PASS |
| Helper macOS | 13 testów i Swift state PASS; build/architektura x64 oraz arm64 PASS |
| Dokumentacja | Build PASS |

Nie uruchamiano publikowania ani wdrożenia. Pozostałe platformy natywne oraz końcowy required gate muszą przejść w zdalnym CI dokładnego commita PR.
