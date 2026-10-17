# OB-6 — decyzja człowieka i trwała historia

Data odbioru: 2026-10-17. Branch `feature/OB-6`, baza `3324f32`.
Status: implementacja, niezależny review i lokalne bramki zakończone; publikacja PR oraz weryfikacja zdalnego CI w toku. Epika OB-1 pozostaje otwarta.

## Zachowanie

Po pozytywnych testach i review workflow oczekuje na człowieka. Akceptacja wymaga osobnego, natywnego dialogu Electrona i aktualnych dowodów dla wskazanego cyklu, snapshotu, operacji testów i review. SQLite zapisuje decyzję z komentarzem atomowo ze zmianą wersji przebiegu. Powtórzenie tego samego żądania odczytuje poprzedni wynik; nie powiela decyzji ani nie zatwierdza nowszych plików.

Odesłanie wymaga komentarza, uruchamia poprawkę Codexa i wymusza nowe testy oraz review. Na limicie zapisuje komentarz i blokuje dostarczenie poprawki do jawnego zwiększenia limitu. Zmiana plików, także cofnięta, unieważnia aktualność. Restart zachowuje historię, ale nie odtwarza utraconej obserwacji ani zgody.

Prywatna intencja powstaje w procesie main, ma 32 losowe bajty i ważność 60 sekund. Renderer nie otrzymuje tokenu. IPC przyjmuje wyłącznie zaufaną główną ramkę lokalnego okna. Demon potwierdza wyzwanie przez istniejący prywatny broker; zwykłe HTTP i publiczne akcje przeglądarki nie mogą zastąpić dialogu. Listener LAN odrzuca trasę decyzji przed autoryzacją, również dla sfałszowanego Host localhost. Funkcja produktu nie wykonuje commit, push, merge ani deploy.

## Dowody automatyczne

| Obszar | Dowód |
| --- | --- |
| Atomowość, CAS, retry, CDC i restart SQLite | `TestWorkflowDecisionAtomicVersionConflict`, `TestWorkflowDecisionRetryIdentity`, `TestWorkflowDecisionSurvivesReopen`; pełne zestawy store i migracji. |
| Aktualne testy, review, wykonawca i wersja | `TestWorkflowHumanAcceptanceRequiresCurrentEvidence`; brak potwierdzenia, odmowa, obcy snapshot/test/review, zmieniona wersja, wymagane uwagi, zakończona sesja i zmiana plików nie zapisują zgody. |
| Zmiana podczas dialogu | `TestWorkflowHumanDecisionRechecksAfterConfirmation`: zmiana plików i Stop, bez przetrzymywania blokad podczas oczekiwania na człowieka. |
| Decyzja i kolejna poprawka | `TestWorkflowHumanDecisionRetry`, `TestWorkflowUserCorrectionSource`, `TestWorkflowNewUserCorrectionAfterCompletedFix`; osobna tożsamość poprawki użytkownika, limit i brak podwójnego dostarczenia. |
| Oczekująca poprawka po utracie procesu | `TestWorkflowPendingUserCorrectionSurvivesRecovery`: restart przed rezerwacją i zmiana generacji wykonawcy; sprawdzenie dokładnego komentarza i pierwotnego zadania, nowego cyklu oraz retry starej decyzji. |
| Prywatne potwierdzenie | Host main/link Vitest: fałszywe i zmienione intencje, ramka IPC, anulowanie, granica 60 sekund, utrata okna, abort, duplikaty i limit Unicode. Adapter Go sprawdza ścisłe `{confirmed:true}` i błędy brokera. |
| HTTP/LAN | Ścisłe DTO, standardowe błędy i request ID, brak tokenu w odpowiedzi, konflikt, odmowa i niedostępność potwierdzenia, budżet HTTP oraz dokładna blokada LAN. |
| Trwały pełny przebieg usług | `TestLocalWorkflowHTTPRunsTestsCorrectionsAndRealReviewService`: rzeczywiste usługi review i SQLite z kontrolowanym sterownikiem modelu/potwierdzeniem; po ponownym otwarciu baza zawiera decyzję, a `accepted=false`. |
| UI | Panel, historia, kolumna Accepted, desktop-only, komentarz, anulowanie, retry i izolacja spóźnionej odpowiedzi wcześniejszej sesji. Akceptacja nie jest ustawiana optymistycznie. |

## Rzeczywisty Electron i modele na macOS

Uruchomiono bieżący checkout z osobną bazą `~/.ao/dev/ob6/data`, profilem `~/.ao/dev/ob6/electron` i plikiem `~/.ao/dev/ob6/running.json`. Demon początkowo PID 86761, `127.0.0.1:3002`; renderer `localhost:5173`. Aplikację uruchomiono po zbudowaniu nowego demona oraz main/preload. Nie używano bazy projektów użytkownika.

Fixture powstał jako zwykły katalog standalone sesji `standalone-1`; rzeczywisty, zalogowany Codex Chat wykonał `git init`, utworzył `sum.js` i `sum.test.js` oraz uruchomił test Node. Nie utworzono commita ani Git worktree. Pomocniczy bootstrap przypisał istniejącą sesję do kontrolowanego projektu Local review w testowej SQLite, aby uniknąć tworzenia worktree przez zwykły spawn projektu. Ścieżka zawiera nazwę `worktrees`, ale jest samodzielnym repozytorium z własnym `.git`, bez powiązanego worktree.

Rzeczywisty Claude Code ocenił nowe, nieśledzone pliki i zwrócił `approved`. Panel pokazał oczekiwanie na człowieka. Pierwszy natywny dialog anulowano: nie powstała decyzja. Drugi potwierdzono z komentarzem odbioru: API zwróciło `stage=accepted`, `accepted=true`, jeden rekord z dokładnymi identyfikatorami dowodów; historia pokazała „Accepted by you”.

Następnie natywnie odesłano komentarz o JSDoc. Codex otrzymał jedną poprawkę, dodał komentarz, wykonał test, a koordynator uruchomił nowe testy i review Claude w cyklu 2. Pierwsza akceptacja pozostała historyczna. Podczas kolejnego dialogu zmieniono i przywrócono plik: kontrola aktualności zwróciła false; potwierdzenie dialogu zakończyło się konfliktem, bez nowej decyzji. Panel pokazał `workspace_changed`. Resume wykonało nową próbę testów i review.

Kolejne odesłanie dodało przypadek `sum(0,0)===0`; Codex i nowa weryfikacja zakończyły się pozytywnie w cyklu 3. Odesłanie na granicy 3/3 zapisało komentarz, pokazało `round_limit` i wyłączone Resume przy niezmienionym limicie. Po jawnym zwiększeniu limitu do 4 koordynator dostarczył oczekującą poprawkę dokładnie raz; Codex odebrał komentarz, lecz zawarte w nim zdanie „czekaj na jawne zwiększenie” zinterpretował jako nadal niespełniony warunek i nie zmienił plików. Koordynator rozpoznał zakończenie tury i wykonał nową weryfikację w cyklu 4. Potwierdzono bramkę limitu i pojedyncze dostarczenie; nie zaliczono wykonania tej dodatkowej prośby przez model. Wcześniejsze poprawki JSDoc i testu zera zostały wykonane rzeczywiście. Po zatrzymaniu tej instancji uruchomiono nowy daemon PID 33541 na tych samych izolowanych danych. Wszystkie cztery decyzje zachowały identyfikatory i komentarze; API zwróciło `current=false`, `accepted=false`, `blockCode=restart`. Sesję fixture bez zarejestrowanego worktree istniejący lifecycle oznaczył jako Terminated i pokazał w Archive; nie zaliczono jej automatycznego odtworzenia kontrolera jako testu pełnego real-agent recovery. Odczyt trwałej historii po restarcie potwierdzono przez API.

Zrzuty natywnego dialogu, historii, poprawki, unieważnienia i limitu znajdują się w ignorowanym katalogu dowodów zadania. Są to zrzuty rzeczywistego okna; nie są nagraniem wideo. Kontrolowane testy usług są opisane oddzielnie od powyższej próby rzeczywistych modeli. Pełna macierz real-agent regression pozostaje zakresem OB-7.

## Kontrole przed publikacją

Użyto Go 1.27.1, Node 24.19.0 i golangci-lint 2.13.2. Pełny frontend: 478 plików, 8070 testów passed i 7 istniejących skipped. Pełne typy i ESLint passed. Przeszły też regeneracja SQL/API bez zmiany hashy, kontrakt CI/actionlint, integracja browser postcondition, dokumentacja, mobile, test świeżej instalacji CLI w kontenerze Linux i kontrole helpera macOS. Build/vet/lint Go passed z zerem uwag; formatowanie 2351 źródłowych plików Go czyste.

Pierwsza kontrola formatowania obejmowała stare ignorowane programy fixture; poprawiono zakres na źródła repozytorium. Typy E2E wykryły dwa kompletne mocki preload bez nowego bridge — uzupełniono je o tryb desktop-unavailable i ponowiono kontrolę. Pierwszy renderer smoke napotkał port 5173 zajęty przez manualny Electron; ponowiono go na osobnym porcie 5186. Pierwszy dry-run pakietów uruchomiony z katalogu głównego wymaga poprawienia cwd na katalog danego pakietu.

Typy E2E po uzupełnieniu fixture passed; renderer smoke na porcie 5186: 83 passed. Oba pakiety przeszły dry-run z własnych katalogów. Końcowy build/vet/lint Go po poprawce review passed (zero uwag). Skan zmienionych plików nie wykrył sekretów. Wszystkie pełne shardy backendu passed: core 521/451 s, SQLite 473/457/666/519 s, Chat 366/195 s oraz session/store 212 s. Natywny CLI E2E macOS passed (24 s). Zdalny CI będzie odczytany dla końcowego commita; Windows regression, CLI na innych OS i natywny helper Intel wymagają faktycznych runnerów CI. Nie są oznaczone jako zaliczone lokalnie. Adres PR i końcowy wynik CI są utrzymywane w Jira OB-6 i dzienniku Confluence. Wydania i produkcyjnego deploymentu nie wykonywano.

## Niezależny przegląd

Jednorazowy przegląd całego zakresu przez osobnego reviewera `gpt-6-astra` wykrył ważny przypadek: zapisane odesłanie mogło zostać pominięte po restarcie lub zmianie generacji przed rezerwacją poprawki. Test regresyjny odtworzył oba warianty (`fixes=0`, cykl 1). Resume zachowuje teraz niewysłane źródło decyzji, a recovery nie konsumuje operacji dla innego źródła. Po poprawce przeszedł cały zestaw `localworkflow` i `localsnapshot` z `-race -count=1` (206.6 s / 13.7 s).

Odroczona drobna uwaga: wygenerowany opis operacji decyzji nie deklaruje oczekiwanych odpowiedzi 403 i 503. Caller procesu main obsługuje te odpowiedzi, a testy HTTP sprawdzają mapowanie; nie jest to możliwość ominięcia akceptacji. Uzupełnienie deklaracji poprawiłoby kompletność generowanych klientów.

Granica bezpieczeństwa nie obejmuje właściciela systemu modyfikującego pamięć procesu lub SQLite. Inne platformy wymagają własnych kontroli natywnego dialogu; wynik macOS nie jest ich dowodem.

Po odbiorze zakończono wyłącznie procesy tej testowej instancji Electron/Forge i demona. Port 3002 jest wolny; testowe dane oraz dowody zachowano. Pełne logi są w ignorowanym katalogu zadania `.superpowers/sdd/2026-10-17-ob-6-human-acceptance/` (po zamknięciu planu archiwum `.cache/ob6-evidence/`).
