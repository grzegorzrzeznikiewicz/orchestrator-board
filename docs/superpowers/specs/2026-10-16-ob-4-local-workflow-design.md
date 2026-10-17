# OB-4 — automatyczna pętla lokalnego wykonania, testów i review

Status: specyfikacja zatwierdzona przez użytkownika 2026-10-16. Plan implementacji również zatwierdzony; użytkownik wybrał wykonanie natywne.

Źródło: [OB-4](https://gamasoftware.atlassian.net/browse/OB-4), odczytane z istniejącej karty Jira 2026-10-16. Opis nie zawiera komentarzy doprecyzowujących.

## Cel i kryteria

Aplikacja prowadzi wykonawcę Codex przez wykonanie zadania, skonfigurowane testy, niezależne lokalne review Claude i wymagane poprawki. Każda poprawka wymaga ponownych testów oraz nowego review. Gotowość do akceptacji użytkownika wymaga pozytywnych testów i aktualnego zatwierdzenia recenzenta dla tej samej zawartości plików. Samo zatwierdzenie Claude nie stanowi akceptacji użytkownika.

Limit rund jest konfigurowalny i domyślnie wynosi 3. Błąd testów, recenzenta lub konfiguracji daje jawną blokadę. Po wyczerpaniu limitu proces zatrzymuje się z ostatnimi wymaganymi uwagami. Użytkownik może zatrzymać proces i świadomie go wznowić. Restart odtwarza historię i stan bez powtórnego uruchomienia tej samej operacji. Żaden etap workflow nie tworzy commita ani PR.

Wymagana walidacja z Jira: pełna pętla z poprawkami, limit rund, nieudane testy, restart, powtórzone zdarzenie i przerwanie agenta.

## Stan projektu i zależności

- OB-2 jest obecne w HEAD `3f4e261`: konfiguracja Local review, komendy testów, snapshoty i obserwacja aktualności.
- OB-3 jest scalone do `origin/main` w `c29627b`. Dostarcza numerowane rundy oraz `LocalReviewResult` z wynikami `approved`, `changes_requested`, `error` i uwagami `required`/`suggestion`.
- Gałąź zadania `feature/OB-4` została na polecenie użytkownika zaktualizowana fast-forward do `c29627b`. Historyczna kopia 42 wcześniejszych lokalnych plików OB-3, manifest SHA-256 i bazowy patch pozostają w ignorowanym `.cache/ob4-baseline/`. OB-3 jest bazą implementacji, nie zmianą wchodzącą do zakresu OB-4.
- Praca odbywa się w rzeczywistym katalogu projektu, bez dodatkowych worktree, commitów i push.

## Wybór architektury

Rekomendacja: koordynator lokalnego workflow w Go, korzystający z istniejących usług wykonawcy, silnika review i SQLite. Demon jest jedynym właścicielem przejść. Electron wyświetla stan i wysyła polecenia.

Rozważone alternatywy:

1. Rozbudowa `autoreview.Coordinator`: obecny koordynator operuje na PR i SHA oraz nie zarządza testami ani dostarczeniem poprawek. Połączenie obu mechanizmów wymieszałoby odmienne warunki aktualności i odzyskiwania.
2. Sterowanie pętlą w rendererze: zamknięcie aplikacji lub utrata połączenia przerywałoby nadzór, a dwa okna mogłyby uruchamiać tę samą rundę. Nie spełnia trwałości wymaganej przez Jira.

Nowy koordynator korzysta z obecnych granic usług. Nie wywołuje adapterów agentów z HTTP ani z frontendu i nie otwiera równoległej ścieżki publikacji wyników.

## Konfiguracja i uruchomienie

Konfiguracja projektu otrzymuje dodatni limit rund lokalnego workflow, domyślnie 3. Ustawienia Local review pokazują limit obok uporządkowanej listy komend testów. Stare projekty zachowują obecne działanie.

Start jest jawną akcją dla istniejącej sesji wykonawcy. Wymaga projektu Local review, wykonawcy Codex, recenzenta Claude i co najmniej jednej niepustej komendy testowej. OB-2 pozwala zachować pustą listę do ręcznego review; automatyczny workflow przy pustej liście zgłasza brak konfiguracji zamiast uznawać testy za zaliczone.

Start zapisuje kopię zadania, kryteriów przekazanych w zadaniu, konfiguracji testów i limitu dla danego przebiegu. Zmiana ustawień projektu nie zmienia po cichu uruchomionego przebiegu. Jedna sesja może mieć najwyżej jeden niezamknięty przebieg. Jeżeli wykonawca już realizuje zadanie, workflow obserwuje tę pracę zamiast wysyłać zadanie ponownie. Przy stanie bezczynności można rozpocząć weryfikację bieżących zmian.

## Trwałe fakty i przejścia

Nowa migracja dodaje rekord przebiegu oraz rekordy operacji i wyników testów. Zapis obejmuje identyfikator przebiegu, sesję, konfigurację, numer cyklu, wersję przejścia, żądanie zatrzymania, przyczynę blokady, identyfikator dostarczenia do Codexa, powiązany obrót agenta, identyfikator review i snapshotu oraz czas rozpoczęcia/zakończenia operacji. Stan prezentowany na tablicy sesji nadal jest wyliczany z faktów; nie dodajemy drugiego zapisanego statusu sesji.

Etapy procesu:

1. Oczekiwanie na potwierdzone zakończenie wykonawcy.
2. Testy dla bieżącego stanu zmian.
3. Przygotowanie i uruchomienie lokalnego review.
4. Odczyt zatwierdzonego strukturalnego wyniku OB-3.
5. Przy `changes_requested`: dostarczenie wymaganych uwag do Codexa, oczekiwanie na zakończenie poprawek i przejście do kolejnego cyklu.
6. Przy aktualnym `approved` i zgodnych pozytywnych testach: gotowość do akceptacji użytkownika.

Błąd testów blokuje automatyczne przejście do review. Błąd wykonawcy lub recenzenta, niedostępny agent, niekompletny wynik i utrata aktualności dają blokadę z przyczyną. Nie tworzą pozytywnego wyniku.

Każda operacja otrzymuje trwały identyfikator. Zmiana etapu następuje warunkowo dla oczekiwanej wersji przebiegu. Powtórzone powiadomienie, sweep albo żądanie HTTP nie uruchamia ponownie już zarezerwowanej operacji. Przejścia są najpierw zapisywane, a następnie wykonywane. Wyniki spóźnione z wcześniejszej generacji nie mogą wznowić zatrzymanego lub nowszego etapu. CDC pozostaje oparte na triggerach SQLite.

## Testy i tożsamość plików

Demon wykonuje komendy kolejno w katalogu sesji, z konfiguracją środowiska projektu, limitowanym czasem i możliwością anulowania. Zapisuje polecenie, kod wyjścia, czasy, stdout/stderr lub ścieżkę trwałego logu pod katalogiem danych AO. Przerwane i przekraczające limit czasu polecenia otrzymują osobny wynik, nigdy sukces.

Przed uruchomieniem testów wykonawca musi być bezczynny i nie mieć pracy w kolejce. Obecna bramka `ports.LocalReviewWorkspace` koordynuje start z dostarczaniem pracy do wykonawcy. Obserwacja zmian musi obejmować cały okres testów, przygotowania snapshotu i review; blokada mutexem nie jest utrzymywana przez całe wykonanie zewnętrznego procesu.

Testy wiążemy z identyfikatorem manifestu i ciągłością obserwacji. Zmiana plików podczas testów, również zmiana i przywrócenie poprzednich bajtów, unieważnia wynik. Snapshot review musi odpowiadać stanowi przetestowanemu. Zmiana między testami a snapshotem blokuje przejście; nie dopisujemy sukcesu do nowego stanu. Testy modyfikujące śledzone pliki wymagają uporządkowania zmian i ponownej próby.

Gotowość do akceptacji jest aktualna tylko przy zgodności testów, snapshotu, wyniku review i bieżących plików. Ponowna edycja unieważnia ją zgodnie z OB-2.

## Poprawki, sugestie i limit

Wiadomość do Codexa zawiera pierwotne zadanie oraz wymagane uwagi wraz z lokalizacją i uzasadnieniem. Sugestie są widoczne w historii i wyraźnie oznaczone jako opcjonalne; nie stanowią warunku następnej rundy ani powodu automatycznej poprawki.

Każdy cykl obejmuje testy i co najwyżej jedno review. Pierwsza weryfikacja ma numer 1. Po wymaganych poprawkach rozpoczyna się kolejny cykl. Numer cyklu workflow jest niezależny od globalnego numeru historycznego review z OB-3.

Po wymaganych uwagach w ostatniej dozwolonej rundzie workflow nie wysyła następnej pracy do Codexa. Zapisuje przyczynę zatrzymania i podsumowanie pozostałych wymaganych uwag. Wznowienie nie zeruje licznika. Po wyczerpaniu limitu świadome wznowienie wymaga jawnego zwiększenia limitu; historia pozostaje zachowana.

## Zatrzymanie, restart i niepewne wykonanie

Zatrzymanie najpierw utrwala zamiar użytkownika i blokuje nowe przejścia, następnie anuluje aktywne testy albo działanie agenta przez istniejące granice usług. Proces nie przechodzi dalej, dopóki nie potwierdzi zakończenia aktywności. Niedostępność mechanizmu anulowania jest jawną blokadą.

Po restarcie koordynator rekoncyliuje zapisane identyfikatory z trwałymi faktami sesji, obrotu Codexa i review. Istniejącej potwierdzonej operacji nie zleca drugi raz. Wykonanie o niepotwierdzonym dostarczeniu zatrzymuje jako niepewne zamiast zgadywać, czy wystartowało.

Przerwane testy nie są automatycznie powtarzane: ich wynik pozostaje nieznany do świadomego wznowienia. Nie wznawiamy testów, jeżeli wcześniejszy proces może nadal działać. Utrata ciągłości obserwacji snapshotu przy restarcie blokuje uznanie starego review za aktualne, zgodnie z OB-2/OB-3. Świadome wznowienie przygotowuje nową próbę po sprawdzeniu, że poprzednie działania zakończono. Nowa próba zachowuje osobny identyfikator i poprzednią historię.

## API i interfejs

Usługa workflow udostępnia odczyt, start, zatrzymanie i wznowienie dla sesji. Polecenia są odporne na powtórzenie żądania; niezgodna wersja lub próba równoległego przebiegu zwraca konflikt. Specyfikacja OpenAPI i klient TypeScript są generowane po zmianie źródeł.

W istniejącej sekcji lokalnego review interfejs pokazuje aktualny etap, cykl i limit, przyczynę blokady, wyniki komend z logami i ostatnie wymagane uwagi. Przyciski umożliwiają start, zatrzymanie i jawne wznowienie. Działanie jest dostępne po ponownym otwarciu aplikacji. Nowe ekrany Kanban i akceptacji pozostają odpowiednio w OB-5 i OB-6.

Instrukcje wysyłane przez workflow wymagają pozostawienia zmian bez commitów i PR. Workflow nie wywołuje operacji Git zapisujących historię ani dostawcy PR. Nie usuwa istniejących instrukcji użytkownika dotyczących pracy wykonawcy.

## Miejsca integracji potwierdzone w kodzie

- `backend/internal/domain/projectconfig.go`: `ReviewMode`, `TestCommands`, walidacja i wartości domyślne.
- `backend/internal/ports/local_review.go`: wspólna bramka pracy wykonawcy i przygotowania review.
- `backend/internal/review/local_review.go`: snapshoty, wymóg bezczynności, blokada domyślnego automatycznego triggera lokalnego review, unieważnianie i timeout.
- `backend/internal/service/review/local_result.go`: walidacja i trwały zapis wyniku OB-3; duplikat poprawnego zgłoszenia jest już rozpoznawany.
- `backend/internal/service/chat/service.go` i `controller.go`: wysyłanie z identyfikatorem wiadomości, trwałe obroty, zakończenie, przerwanie i zatrzymanie.
- `backend/internal/session_manager/manager.go`: istniejące dostarczanie pracy i bramka lokalnego review dla sesji terminalowych.
- `backend/internal/daemon/lifecycle_wiring.go`: powiązanie usług i cykl życia silnika review.
- `frontend/src/renderer/components/LocalReviewHistory.tsx` i `ProjectSettingsForm.tsx`: historia review i ustawienia projektu.

## Weryfikacja przed odbiorem

Testy zachowania należy pisać przed implementacją. Kluczowe scenariusze:

- Codex kończy pracę, testy przechodzą, Claude wymaga poprawki, Codex ją wykonuje, nowe testy i review przechodzą; dopiero wtedy gotowość do akceptacji.
- Wymagane uwagi ostatniej rundy zatrzymują proces; sugestia przy zatwierdzeniu go nie blokuje.
- Nieudana komenda, timeout, brak konfiguracji, niedostępny Claude i przerwany Codex blokują przejście z czytelną przyczyną.
- Zmiana plików podczas testów i między testami a review unieważnia dowód, również po przywróceniu poprzedniej treści.
- Ponowiony start, powtórzone zdarzenie i równoległy sweep nie duplikują testu, review ani wiadomości do wykonawcy.
- Zamknięcie i ponowne otwarcie rzeczywistej bazy SQLite odtwarza historię; rekoncyliacja nie uruchamia ponownie tej samej operacji.
- Zatrzymanie w każdym aktywnym etapie blokuje następny, spóźniony wynik go nie odblokowuje, świadome wznowienie zachowuje licznik i historię.
- Regresja ręcznego Local review i dotychczasowego PR review.

Po testach skupionych: odpowiednie pełne zestawy backendu i frontendu, kontrola typów, lint, build, testy z detektorem wyścigów dla koordynatora i magazynu, regeneracja API/sqlc oraz rzeczywista aplikacja Electron z osobnymi danymi pod `~/.ao/dev/`. Próba z rzeczywistymi agentami jest odrębna od testów z podstawionymi granicami procesów i musi być wyraźnie oznaczona w raporcie.

## Granica obecnego etapu

Dokument opisuje zatwierdzoną architekturę. Implementacja i testy są prowadzone według [planu](../plans/2026-10-16-ob-4-local-workflow.md). Wyniki oraz ograniczenia weryfikacji znajdują się w [raporcie OB-4](../../ob-4-verification.md); zmiany pozostają bez commitów.
