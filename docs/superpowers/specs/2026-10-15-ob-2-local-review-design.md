# OB 2 Tryb Local review i identyfikacja zmian
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->


Zrealizowany projekt techniczny. Celem jest spełnienie kryteriów [OB-2](https://gamasoftware.atlassian.net/browse/OB-2) w rozwijanej wersji Agent Orchestrator. Projekt zachowuje istniejący tryb review PR i dodaje lokalny cel review, związany z konkretną zawartością plików.

## Baza projektu

Baza to stabilne [wydanie v0.14.2](https://github.com/OrchestratorInc/agent-orchestrator/releases/tag/v0.14.2). Estymacja w OB-1 dotyczyła v0.13.3; Wybrany SHA to `55b2962a5f7478fa7570777aad92f3bef3c99ccd`. Aktualizacje upstreamu nie należą do OB-2.

Na początku katalog zawierał wyłącznie AGENTS.md i był objęty repozytorium nadrzędnym GamaSoftware. Historię upstreamu pobrano do własnego repozytorium w OrchestratorBoard, a branch `feature/OB-2` wskazuje istniejący commit wydania. Zachowano instrukcje lokalne oraz informacje i licencję upstreamu. Zmiany zadania pozostają bez commitów i push. Nie użyto dodatkowych worktree ani repozytorium nadrzędnego do realizacji zadania.

## Zakres i granice zadania

Ustawienia projektu pozwalają wybrać PR review albo Local review. Local review zapisuje wykonawcę Codex, recenzenta Claude i uporządkowaną listę komend testów. Konfiguracja korzysta z istniejącego modelu projektu i SQLite. Komendy testów są w OB-2 konfigurowane i walidowane; automatyczne wykonywanie pętli testów i poprawek należy do OB-4.

Lokalne review można uruchomić bez PR/MR oraz bez połączenia ze zdalnym dostawcą repozytorium. OB-2 zapewnia wybór lokalnego celu, przygotowanie spójnego stanu, zapis rundy i kontrolę aktualności. Dedykowany protokół wyników Claude i ich obsługa należą do OB-3. Pełna pętla, Kanban i ekran akceptacji pozostają w OB-4, OB-5 i OB-6. Próba odbiorowa OB-2 musi wykazać faktyczne przejście lokalnego żądania do granicy uruchamiania recenzenta; sama obecność przycisku nie wystarcza.

## Wybrane podejście

Rozszerzyć istniejący model review i usługi demona o jawny rodzaj celu `pr` lub `local`, zamiast tworzyć równoległy system review. Wprowadzić osobne pole identyfikatora lokalnego stanu; nie wykorzystywać `targetSha` do przechowywania wartości innej niż commit PR. Puste ustawienie trybu w dotychczasowych projektach oznacza PR review.

Alternatywa z osobnym systemem lokalnych review dublowałaby przechowywanie rund i obsługę recenzentów. Alternatywa oparta wyłącznie na obecnym `workspaceVersion` nie spełnia wymogu identyfikacji treści: `summaryFingerprint` w v0.14.2 uwzględnia nazwę, status, rozmiar i statystyki zmian, więc dwie różne treści mogą otrzymać ten sam identyfikator.

Logika identyfikacji, spójności i aktualności pozostaje w Go. Interfejs Electron/React wyświetla dane i wysyła polecenia przez istniejące API demona.

## Identyfikacja zawartości

Identyfikator SHA-256 obejmuje wersję formatu, tożsamość repozytorium, bazowy commit, HEAD oraz uporządkowany manifest plików. Manifest uwzględnia ścieżkę, typ i tryb pliku, obecność lub usunięcie oraz skrót rzeczywistej zawartości. Stan indeksu jest uwzględniany, aby rozróżniać staged i unstaged. Weryfikacja obejmuje także śledzone pliki kontekstu, a nie tylko fragmenty diffu.

Nowe pliki nieignorowane przez Git wchodzą do manifestu i snapshotu. Śledzone pliki pozostają uwzględnione również wtedy, gdy pasują do reguły ignorowania. Pliki ignorowane i nieśledzone pozostają poza zakresem review. Usunięcie, zmiana nazwy, zmiana uprawnień, zmiana celu dowiązania i edycja zachowująca rozmiar zmieniają identyfikator. Czasy modyfikacji i kolejność listowania nie zastępują skrótów zawartości.

Dowiązania są identyfikowane po celu, bez odczytywania plików poza repozytorium. Nieobsługiwane zagnieżdżone repozytoria, submoduły ze zmianami lub błędy odczytu powodują jawny błąd przygotowania, zamiast pozornie kompletnego review. Limity zasobów muszą kończyć przygotowanie błędem; nie wolno obliczać aktualnego identyfikatora z uciętego diffu.

## Spójny snapshot i zakończenie edycji

Uruchomienie Local review wymaga potwierdzonego zakończenia bieżącej pracy wykonawcy. Stan aktywny albo nieznany blokuje start. Blokada usługi dla danej sesji zapobiega równoczesnemu rozpoczęciu kolejnego etapu edycji i review.

Po zakończeniu edycji demon tworzy kopię zawartości do review w izolowanym katalogu danych testowych lub aplikacji, bez dodatkowego Git worktree. Manifest i skróty kopii muszą odpowiadać odczytanemu stanowi źródła. Zmiana podczas kopiowania unieważnia przygotowanie. Kopia snapshotu nie jest miejscem pracy wykonawcy.

Runda zapisuje identyfikator snapshotu i generację zmian przed wywołaniem recenzenta. Uruchomienie kolejnej edycji lub wykrycie zmiany plików oznacza trwałe unieważnienie tej rundy jako bieżącej; powrót do tej samej zawartości nie przywraca jej automatycznie. Zewnętrzne edycje podczas review muszą być obserwowane, a przed przyjęciem wyniku i przy odczycie jego aktualności demon ponownie porównuje manifesty. Błąd obserwacji lub odczytu nie oznacza zgodności.

Recenzent ocenia przygotowaną kopię. Nawet gdy źródło zmieni się w trakcie review, zapis historycznego wyniku nadal odnosi się do znanej zawartości, lecz wynik nie jest aktualny dla projektu. Restart nie przywraca ważności rundzie, dla której nie można potwierdzić ciągłości obserwacji; konfiguracja i zapisany identyfikator pozostają dostępne.

## Trwałość i migracja

Nowa migracja SQLite dodaje rodzaj celu oraz dane lokalnego snapshotu i unieważnienia. Istniejące rekordy otrzymują rodzaj PR, zachowując URL, SHA, wyniki, identyfikatory recenzentów i historię. Nie zmieniać wcześniejszych migracji.

Indeksy i wyszukiwanie rund rozróżniają rodzaj celu. Rekord bez URL nie może zostać uznany za review PR; lokalna runda nie korzysta z połączeń SQL wymagających tabeli PR. Zapytania należy zmienić u źródła i odtworzyć kod generowany przez sqlc.

Aktualność wyniku i akceptacji zależy od zgodności z lokalnym identyfikatorem i braku unieważnienia. OB-2 dostarcza ten warunek dla późniejszego ekranu akceptacji. Nie tworzy automatycznej akceptacji użytkownika ani pełnego interfejsu OB-6. Test kontraktu ważności musi wykazać, że akceptacja powiązana z poprzednim stanem nie jest ważna po zmianie plików.

## Interfejs i API

Ustawienia projektu rozszerzają `ProjectSettingsForm` o tryb review i komendy testów. Wybór Local review ustawia lub pokazuje wykonawcę Codex i recenzenta Claude, korzystając z istniejących pól ról tam, gdzie zachowuje to jednoznaczną konfigurację. Zapis i ponowne wczytanie pokazują te same wartości. Nieznany tryb i puste komendy są odrzucane; lista komend może być pusta, jeśli projekt nie ma testów.

Lokalna akcja review nie żąda URL. API zwraca rodzaj celu, identyfikator ocenianego stanu i aktualność wyniku. Aktywny wykonawca, zmiana podczas przygotowania, brak pliku lub błąd Git mają czytelne błędy. Zakończenie recenzenta z błędem lub brak wyniku nigdy nie oznacza pozytywnego review.

Zmiany obejmują model domenowy, usługi projektu i review, magazyn SQLite, kontrolery i generowaną specyfikację API oraz typy frontendu. Zachować istniejące ścieżki uruchamiania i odczytu review PR.

## Weryfikacja kryteriów akceptacji

| Kryterium OB-2 | Dowód wymagany do odbioru |
| --- | --- |
| Projekt bez PR/MR uruchamia lokalne review | Test usługi i API na lokalnym repozytorium bez remote i rekordów PR; granica recenzenta otrzymuje lokalny snapshot. |
| Konfiguracja i identyfikator pozostają po restarcie | Zapis do prawdziwego pliku SQLite, zamknięcie i ponowne otwarcie magazynu; porównanie konfiguracji, identyfikatora i historii. |
| Edycja, dodanie i usunięcie unieważniają wynik i akceptację | Testy na prawdziwym tymczasowym repozytorium Git: staged, unstaged, untracked, usunięcia, nazwy ze spacjami i edycja tej samej długości; osobny test edycji i przywrócenia podczas review. |
| Wykonawca kończy edycję przed review | Test blokady startu przy aktywnym i nieznanym stanie wykonawcy; kontrolowany wyścig rozpoczęcia edycji i review. |
| Zmiana w trakcie review uniemożliwia uznanie wyniku za aktualny | Kontrolowana zmiana podczas kopiowania oraz po starcie recenzenta; historyczny wynik może być zapisany, ale pozostaje nieaktualny. |
| Migracja zachowuje review PR | Baza sprzed migracji z zapisanym review PR; po migracji ten sam URL, SHA, werdykt i historia, plus regresja dotychczasowych ścieżek PR. |
| Walidacja konfiguracji | Testy walidatora, round trip API i formularza: nieznany tryb, puste komendy, brak ustawienia w starym projekcie. |

Testy baz danych i identyfikacji korzystają z tymczasowych danych. Nie uruchamiać migracji na używanej bazie `~/.ao`. Po testach skupionych uruchomić właściwe pełne zestawy testów backendu i frontendu, kontrolę typów, kompilację oraz kontrolę zgodności generowanego API. Wyniki i ograniczenia środowiska zapisać w raporcie odbioru; samo przygotowanie tej propozycji nie spełnia kryteriów OB-2.

## Źródła implementacyjne

- [Epik OB-1](https://gamasoftware.atlassian.net/browse/OB-1) i [zadanie OB-2](https://gamasoftware.atlassian.net/browse/OB-2).
- [ProjectConfig w v0.14.2](https://github.com/OrchestratorInc/agent-orchestrator/blob/v0.14.2/backend/internal/domain/projectconfig.go).
- [Review i ReviewRun](https://github.com/OrchestratorInc/agent-orchestrator/blob/v0.14.2/backend/internal/domain/review.go).
- [Dotychczasowe obliczanie workspaceVersion](https://github.com/OrchestratorInc/agent-orchestrator/blob/v0.14.2/backend/internal/service/session/workspace_review.go).
- [Silnik review](https://github.com/OrchestratorInc/agent-orchestrator/blob/v0.14.2/backend/internal/review/review.go).
- [Ustawienia projektu](https://github.com/OrchestratorInc/agent-orchestrator/blob/v0.14.2/frontend/src/renderer/components/ProjectSettingsForm.tsx).

## Doprecyzowania po implementacji i przeglądzie

- Snapshot zawiera `files/`, manifest oraz trzy kompletne patche: względem HEAD, staged i unstaged. Oddzielny patch staged pokazuje także zawartość ukrytą przez późniejszą edycję pliku roboczego.
- Wysłanie nowej pracy do wykonawcy unieważnia lokalne wyniki przed przekazaniem jej agentowi. Terminal i native chat współdzielą bramkę przygotowania snapshotu; kolejka native chat również blokuje start review.
- Utrata obserwacji przy restarcie unieważnia zarówno review w toku, jak i wcześniejsze zakończone wyniki. Identyfikator, konfiguracja i treść historyczna pozostają trwałe. Nowe review jest jawną akcją użytkownika.
- Obserwator obejmuje istniejące puste katalogi. Dodatkowy odczyt metadanych katalogów wykrywa szybkie create/delete, które kqueue na macOS może scalić. Istniejące ignorowane pliki i przypadkowe pliki Git `.lock` nie wywołują unieważnienia; utworzenie/usunięcie ignorowanego pliku może ostrożnościowo unieważnić wynik przez zmianę metadanych katalogu.
- SQL odmawia zapisu zatwierdzenia po unieważnieniu, a usługa ponownie sprawdza aktualność po zapisie. Żadna z tych ścieżek nie tworzy akceptacji użytkownika.
- Lokalnych reviewerów nie odtwarza się ścieżką PR po restarcie. Historia jest dostępna, a nowe uruchomienie przygotowuje nowy snapshot i lokalny prompt.
