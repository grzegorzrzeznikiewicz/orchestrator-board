# OB-6 — decyzja użytkownika o lokalnym workflow

Status: kierunek i niniejsza szczegółowa specyfikacja zatwierdzone przez użytkownika 2026-10-17. To projekt, nie raport wykonania. Implementacji i testów OB-6 jeszcze nie wykonano. Bazowe testy istniejących pakietów `localworkflow`, `localsnapshot` i `browserruntime` przeszły z `-race -count=1`; nie stanowią testów nowej funkcjonalności.

Źródło: [OB-6](https://gamasoftware.atlassian.net/browse/OB-6), opis i kryteria odczytane z Jira 2026-10-17. Baza: `origin/main` `3324f32dc96d0b22becafb499985f73d54f29228`, po scaleniu [OB-5 / PR #7](https://github.com/grzegorzrzeznikiewicz/orchestrator-board/pull/7). Branch: `feature/OB-6` w rzeczywistym katalogu projektu. Nie tworzymy dodatkowych worktree.

## Cel i granice

Po aktualnych, pozytywnych testach i niezależnym review aplikacja czeka na człowieka. Użytkownik wybiera **Akceptuj** albo **Odeślij do poprawek**, z komentarzem. Decyzja, czas i dokładny zestaw dowodów trafiają do trwałej historii. Review Claude nie może samo nadać akceptacji człowieka.

Akceptacja nie wykonuje commit, push, merge ani deploy w projekcie obsługiwanym przez aplikację. Osobną zgodą użytkownika w tej rozmowie jest publikowanie implementacji zadań epiki: branch, kontrole, commit, push i PR dla każdego zadania przed rozpoczęciem następnego. Automatyczne scalanie PR nie jest częścią tej zgody.

Poza OB-6 pozostają akceptacja mobilna, rebranding, źródło aktualizacji i końcowa przekrojowa próba rzeczywistych modeli. OB-6 obejmuje własne testy integracyjne i manualny odbiór decyzji w rzeczywistym Electronie.

## Stan potwierdzony w kodzie

- `backend/internal/localworkflow/coordinator.go`: demon steruje operacjami, wersjami i obserwacją plików; `WorkflowReady` oznacza gotowość do decyzji. `snapshot()` wylicza `Current`, a `presentation.go` etap Kanbana. Nie ma trwałej decyzji człowieka.
- `backend/internal/localworkflow/control.go` i `recovery.go`: Stop/Resume, limit i bezpieczne odzyskiwanie. Restart obecnie unieważnia ciągłość testów/review; nie wolno jej odtworzyć z samej zgodności bajtów.
- `backend/internal/localsnapshot/guard.go`: monotoniczne wykrywanie zmian, także zmiany i przywrócenia treści; błąd obserwacji nie jest sukcesem.
- `backend/internal/storage/sqlite/migrations/0201_local_workflow.sql`: trwałe przebiegi, operacje i testy; aktualizacja przebiegu emituje CDC przez trigger. Migracja jest już scalona i pozostaje niezmieniona.
- `frontend/src/renderer/components/LocalWorkflowPanel.tsx`: panel Reviews ma Start/Stop/Resume i historię. `LocalWorkflowBoard` ma kolumnę Accepted, która nie jest jeszcze zasilana decyzją człowieka.
- `backend/internal/browserruntime/broker.go` i `frontend/src/main/browser-runtime-link.ts`: istniejący prywatny kanał z tokenem przekazanym do demona przez odziedziczony stdin. Token nie trafia do rendererów, środowiska wykonawcy ani pliku.
- `frontend/src/main.ts`: rejestr zaufanych shell WebContents, obsługa natywnych dialogów i komend wewnętrznych brokera. Publiczna usługa browser ma zamkniętą listę akcji, która nie zawiera komend wewnętrznych.

## Wybór rozwiązania

Rozszerzamy istniejący koordynator, dodajemy tabelę decyzji i wąską granicę potwierdzenia w Electronie. Renderer prezentuje dane; demon sprawdza dowody i utrwala decyzję; proces main potwierdza intencję w natywnym dialogu. Wykorzystujemy istniejący broker zamiast nowego transportu.

Alternatywa z bezpośrednim zapisem przez publiczne HTTP albo polem `actor=user` w żądaniu nie daje dowodu decyzji człowieka. Alternatywa z nowym socketem i niezależnym sekretami duplikuje działający transport. Rozszerzanie watchdogowego `supervise.sock` miesza decyzje produktu z mechanizmem żywotności procesu.

## Intencja i potwierdzenie człowieka

1. Lokalny panel Reviews wywołuje wąską metodę preload IPC z sesją, akcją, komentarzem, identyfikatorem żądania, wersją przebiegu i wskazanymi dowodami. Brak metody w środowisku web oznacza niedostępność akcji, a nie alternatywną drogę zapisu.
2. Main sprawdza nadawcę: dokładne zaufane WebContents i jego główną ramkę. Odrzuca BrowserView, podramki, zdalne hosty i niepoprawne dane. Generuje losowy identyfikator intencji (32 bajty z generatora kryptograficznego) i przechowuje w pamięci pełną treść zamiaru, właściciela okna i termin ważności 60 sekund. Renderer nie otrzymuje tego identyfikatora.
3. Main wysyła żądanie do lokalnego demona z aktualnego, zweryfikowanego połączenia. Do requestu dodaje prywatny identyfikator intencji. Nie przyjmuje od renderera adresu serwera ani nagłówków.
4. Demon sprawdza przebieg i dowody, następnie przez istniejący uwierzytelniony broker prosi main o potwierdzenie wewnętrzną komendą. Main wymaga zgodności całego zamiaru z oczekującą intencją, poprawnego okna, nieprzekroczonego terminu i nieanulowanego połączenia.
5. Natywny dialog pokazuje sesję, akcję, rundę, identyfikator sprawdzanych zmian i komentarz jako zwykły tekst. Domyślną oraz anulującą odpowiedzią jest Anuluj. Brak aktywnego właściciela okna nie może zamienić potwierdzenia w automatyczną zgodę.
6. Wyłącznie jawna odpowiedź potwierdzająca wraca przez uwierzytelniony kanał. Jedna intencja może potwierdzić tylko jeden zamiar; powtórzenia tego samego żądania nie otwierają równoległych dialogów. Zmiana treści, wersji, sesji lub akcji wymaga nowej intencji i potwierdzenia.
7. Po dialogu demon ponownie sprawdza wersję, dowody i pliki. Dopiero wtedy atomowo zapisuje decyzję i kolejny checkpoint. Nie trzyma blokady koordynatora ani bramki workspace przez czas oczekiwania na człowieka.

HTTP transportuje prośbę o decyzję, nie deklarację uprawnienia. Bez potwierdzenia na prywatnym kanale zapis jest niemożliwy. Nie dodajemy uwierzytelnienia do głównego listenera HTTP, nie zmieniamy jego adresu i nie tworzymy nowego listenera. Endpoint decyzji jest niedostępny przez listener mobilny/LAN, zgodnie z istniejącym wydzieleniem lokalnych tras kontrolnych. Nie przekazujemy tej akcji do publicznej listy komend browser ani narzędzi agentów.

Brak Electrona/brokera, utrata połączenia, przekroczenie 60 sekund od utworzenia intencji, zamknięcie okna, anulowanie i niezgodna intencja nie zapisują decyzji. Odpowiedź na dialog po tym terminie nie zatwierdza zapisu; użytkownik może ponowić akcję. Sekrety i identyfikatory intencji nie mogą trafić do odpowiedzi API, logów ani trwałej historii. Historia oznacza źródło `desktop_user`, nadane przez serwer po potwierdzeniu; nie przyjmuje takiej deklaracji od klienta.

Ta granica uniemożliwia samodzielne nadanie akceptacji przez wynik review, standardowe narzędzia agenta lub wywołanie HTTP. Nie stanowi izolacji od procesu z uprawnieniami do odczytu pamięci aplikacji, modyfikacji bazy albo automatyzacji całego systemu operacyjnego. Tego rodzaju izolacji istniejąca aplikacja nie zapewnia.

## Trwały model decyzji

Nowa migracja `0202_local_workflow_decision.sql` tworzy tabelę decyzji powiązaną kluczem obcym z przebiegiem. Zawiera identyfikator decyzji i żądania, rodzaj `accepted` / `changes_requested`, komentarz, czas, cykl, wersję wejściową, snapshot, identyfikator review i operacji testów. Identyfikator obserwacji pozostaje prywatnym szczegółem utrwalonego dowodu, jeżeli jest potrzebny do porównania.

Unikalność `(workflow_id, request_id)` zapewnia idempotencję. Powtórzenie z innym zestawem danych zwraca konflikt. Dokładne powtórzenie zapisanej decyzji zwraca istniejący wynik, nie zmienia czasu, nie nadaje akceptacji nowemu review i nie ponawia poprawek. Historia jest dopisywana, a wcześniejsze decyzje pozostają czytelne po unieważnieniu.

Zapytania sqlc i store zapisują decyzję oraz zwiększenie wersji przebiegu w jednej transakcji warunkowej. Nie wprowadzamy drugiego zapisanego statusu sesji. Aktualizacja przebiegu wykorzystuje istniejący trigger CDC. Nie emitujemy ręcznie równoległego zdarzenia. Generowane pliki powstają przez `npm run sqlc`.

`Snapshot` / DTO odpowiedzi otrzymuje historię decyzji oraz wyliczoną informację o aktualnej akceptacji. Etap `accepted` wynika z aktualnej decyzji i aktualnych dowodów; faza wykonania może pozostać `ready`. Nie zamykamy przebiegu przy akceptacji, aby obserwacja i unieważnianie nadal działały.

## Warunki akceptacji i konkurencja

Akceptacja wymaga równocześnie: lokalnego projektu, istniejącej nieterminowanej sesji, fazy `ready`, zgodnej wersji i tożsamości przebiegu, braku Stop, bezczynnego wykonawcy i pustej kolejki, zakończonych operacji, kompletu pozytywnych wyników bieżącej próby testów oraz aktualnego, poprawnego strukturalnie review `approved` bez wymaganych uwag.

Sprawdzamy zgodność cyklu, snapshotu, powiązania review z konkretną operacją testów oraz ciągłości obserwacji. Sama flaga `Current` przesłana przez klienta ani sam identyfikator snapshotu nie są dowodem. Powtórny odczyt i walidacja po natywnym dialogu odbywają się pod istniejącą blokadą przebiegu i bramką dopuszczania pracy do workspace; zapis używa oczekiwanej wersji. Zmiana w trakcie dialogu zwraca konflikt i wymaga odświeżenia.

Operacje systemu plików nie tworzą wspólnej transakcji z SQLite. Po zapisie obserwacja nadal obowiązuje; zmiana wykryta w pobliżu granicy zapisu natychmiast odbiera aktualność akceptacji przy odczycie i rekoncyliacji. Nie obiecujemy zablokowania zewnętrznych edytorów.

## Odesłanie do poprawek

Odesłanie jest decyzją użytkownika na etapie `ready`, przed akceptacją lub po niej, gdy dowody nadal są aktualne. Wymaga niepustego komentarza; akceptacja dopuszcza pusty komentarz. Komentarz ma limit 4000 znaków i nie może zawierać NUL. Odesłanie zapisuje decyzję i odbiera dotychczasową akceptację w tej samej transakcji.

Koordynator wykorzystuje istniejącą ścieżkę dostarczenia poprawek Codexowi. Źródłem nowej operacji jest identyfikator decyzji użytkownika, odróżniony od źródła wcześniejszego review. Rezerwacja i rozpoznawanie bieżącej operacji uwzględniają to źródło, aby stara ukończona poprawka nie pominęła komentarza ani ponowiony sweep nie wysłał go kolejny raz.

Prompt zachowuje pierwotne zadanie, kryteria oraz komentarz użytkownika jako dane do poprawy, bez udawania uwagi Claude i bez awansu opcjonalnych sugestii. Potwierdzone zakończenie Codexa zwiększa cykl, po czym obowiązkowo następują nowe testy i review. Poprzednia akceptacja nie wraca po ponownym uzyskaniu identycznych bajtów.

Limit rund nie zeruje się i nie rośnie automatycznie. Gdy nie ma miejsca na następny cykl, zapisujemy odesłanie i blokadę `round_limit`, bez wysyłania pracy do agenta. Istniejące świadome Resume po zwiększeniu limitu wraca do poprawnego źródła poprawek. Brak wykonawcy lub utrata jego generacji pozostawia widoczną blokadę i historię decyzji.

## Zmiana plików i restart

Aktualność akceptacji jest pochodną aktualności tych samych testów i review oraz ostatniej decyzji dla tych konkretnych operacji. Zmiana nowych lub śledzonych plików, indeksu, HEAD albo utrata obserwacji odbiera status Accepted. Zmiana i przywrócenie poprzedniej treści nie wznawia akceptacji.

Restart zachowuje wszystkie decyzje, komentarze, czasy i powiązania. Utrata ciągłości obserwacji oznacza historyczną akceptację, nie aktualny status Accepted. Obecna semantyka recovery pozostaje bezpieczna: jawne wznowienie, nowe testy, nowe review i nowa decyzja. Ponowne otwarcie samego panelu bez restartu demona nie unieważnia decyzji.

## Interfejs i API

W panelu Reviews pokazujemy przyciski Akceptuj oraz Odeślij do poprawek i pole komentarza, tylko dla lokalnego hosta i desktopowego bridge. Odesłanie bez komentarza jest zablokowane. W trakcie potwierdzenia nie można wielokrotnie uruchamiać akcji. Anulowanie dialogu nie jest pokazywane jako błąd ani sukces.

Przyciski są niedostępne przy nieaktualnych lub niekompletnych dowodach, z czytelną przyczyną. Serwer zawsze ponawia walidację. Konflikt odświeża dane i nie pokazuje optymistycznie akceptacji. Odpowiedź dotycząca wcześniejszej sesji lub hosta nie zmienia bieżącego panelu.

Historia pokazuje rodzaj decyzji, komentarz, czas, cykl, snapshot i powiązane dowody. Nieaktualna akceptacja jest opisana jako historyczna. Karta i kolumna Accepted korzystają z wyliczenia demona. Poprzednie projekty PR zachowują dotychczasowy przepływ.

Źródła kontraktu to `controllers/dto.go` oraz rejestr `apispec/specgen/build.go`. Regenerujemy OpenAPI i typy przez `npm run api`; nie edytujemy wygenerowanych typów ręcznie. Błędy zachowują standardową kopertę i request ID. Konflikt wersji/dowodów to 409, niepoprawne dane 422, brak lub odmowa potwierdzenia ma odrębny stabilny kod; niedostępny kanał nie jest sukcesem.

## Weryfikacja wymagana przed publikacją

Testy zachowania powstają przed implementacją. Obejmują:

- Pozytywne dowody prowadzą wyłącznie do oczekiwania na człowieka; zatwierdzenie Claude i `actor=user` w żądaniu nie nadają akceptacji.
- Natywna zgoda dla zgodnego zamiaru zapisuje dokładnie jedną decyzję; anulowanie, brak brokera, niewłaściwy nadawca IPC, niezgodny zamiar, timeout i replay nie zapisują nowej decyzji.
- Publiczna akcja browser nie może wywołać wewnętrznego potwierdzenia; trasa mobilna/LAN nie udostępnia decyzji.
- Nieudane/niekompletne testy, wymagane uwagi, nieaktualny snapshot, aktywna lub zakolejkowana praca, Stop, obca sesja/przebieg i zmiana podczas dialogu blokują zapis.
- Powtórzenie żądania nie duplikuje historii ani poprawek; zmienione dane z tym samym request ID dają konflikt. Transakcja cofa decyzję przy konflikcie wersji.
- Odesłanie po akceptacji, ukończonej wcześniejszej poprawce i przy limicie przekazuje właściwy komentarz dokładnie raz, wymusza nową weryfikację i zachowuje limit.
- Zmiana plików oraz zmiana/przywrócenie treści odbierają aktualność; ponowne otwarcie SQLite i usług zachowuje historię bez odtworzenia starej aktualności.
- Panel i Kanban pokazują poprawny etap, historyczne decyzje, błędy i komentarze; zmiana właściciela panelu chroni przed spóźnioną odpowiedzią. Dotychczasowy PR workflow pozostaje sprawny.

Po testach skupionych uruchamiamy pełne odpowiednie zestawy z `docs/ci.md`: backend z race, build/vet/golangci-lint, frontend testy/typy/ESLint, kontrolę generacji, CI contract i wymagane zadania zależne od zmienionych ścieżek. Przed PR obowiązuje pełna lokalna walidacja wskazana przez workflow; brak platformy ma być jawnie raportowany i potwierdzony zdalnie. Po push sprawdzamy rzeczywiste CI dla końcowego commita.

Manualny odbiór w rzeczywistym Electronie obejmuje pozytywną decyzję z natywnym dialogiem, anulowanie, odesłanie z komentarzem, zmianę pliku, ponowne otwarcie panelu i restart. Używamy osobnych danych i profilu pod `~/.ao/dev/ob6/`, bez dotykania bazy użytkownika. Raport rozróżnia kontrolowane fixtures od prób rzeczywistych modeli. Materiały bez sekretów trafiają do dziennika Confluence; nagranie dołączamy, jeżeli narzędzia pozwolą je wykonać.

## Odbiór i dalsze zadania

Wynik OB-6 będzie opisany w `docs/ob-6-verification.md`, Jira i [dzienniku epiki](https://gamasoftware.atlassian.net/wiki/spaces/OB/pages/10977281/OB-1+realizacja+i+dowody+odbioru). Gotowość wymaga zaimplementowania kryteriów oraz udokumentowanych wyników; specyfikacja nie zamyka zadania.

Po zatwierdzeniu tej specyfikacji powstanie szczegółowy plan implementacji. Wykonanie pozostaje w bieżącej sesji, bez dodatkowych worktree. Przed commitem/PR uwzględniamy niezależny przegląd zgodnie z procedurą review i weryfikujemy końcowy diff, w tym wymagany nagłówek liczby zmian w opisie PR. Wcześniejsze dokumenty OB-4 pozostają odrębnie zabezpieczone w stashu `OB-1 preserve pre-existing OB-4 documentation before OB-6` oraz `.cache/ob1-handoff/preexisting-ob4.patch`.

Kolejność pozostałych zadań: OB-6 → OB-7 → OB-10 → OB-8 → OB-9. Kolejne zadanie rozpoczynamy po commit, push i utworzeniu PR poprzedniego. Sposób bazowania zależnych PR-ów przed ich scaleniem ustalimy przed zmianą następnej gałęzi; obecna zgoda na PR nie jest zgodą na merge. OB-10 wymaga osobnego potwierdzenia grafiki i własnego źródła/kanałów aktualizacji oraz dostępności podpisywania do pełnego testu aktualizacji. Te dane nie blokują OB-6.
