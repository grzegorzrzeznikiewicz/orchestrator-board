# Automatyczny workflow zadań Local
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->

Zatwierdzone zachowanie: polecenie zadania uruchamia cały proces bez dodatkowego przycisku Start. Dotyczy nowych zadań oraz kolejnych jawnych poleceń użytkownika w istniejącej rozmowie. Dokument rozszerza wcześniejszy kontrakt OB-4 z ręcznym startem.

## Przebieg

1. Przy przyjęciu polecenia demon atomowo zapisuje wiadomość i trwałą intencję workflow. Ponowienie tego samego żądania nie tworzy drugiego zadania. Wiadomości wewnętrzne z poprawkami oraz rozmowy recenzenta nie uzbrajają kolejnego workflow.
2. Koordynator czeka na gotowy katalog i kontroler wykonawcy. Obserwuje zleconą pracę; nie wysyła pierwotnego zadania ponownie.
3. Po zakończeniu wykonania model dobiera jawny plan kontroli na podstawie pierwotnego zadania, najnowszego polecenia i rzeczywistej odpowiedzi. Plan i uzasadnienie są widoczne w aplikacji.
4. Implementacja wymaga testów. Zapisana lista komend projektu jest obowiązkowym minimum, chyba że użytkownik jawnie przypisze do jednego przebiegu kontrakt katalogu opisany poniżej. Aplikacja wykrywa również dostępne kontrole z manifestów JavaScript, Go, Composer i Python (root oraz bezpośrednie podkatalogi, np. backend-py); plan może zawierać dodatkowe testy i kontrole statyczne tego samego zakresu. Model może wybierać istniejące komendy, nie tworzyć poleceń do wykonania. Same lint/typecheck/vet ani oczywiste no-op nie zastępują testów. Niestandardowe TestCommands są jawną deklaracją użytkownika.
5. Dla raportu kontrola obejmuje treść, zakres, źródła i fakty. Brak testów kodu jest jawnie oznaczony jako nie dotyczy; nie zapisujemy fikcyjnych wyników komend. Recenzent otrzymuje faktyczną odpowiedź wykonawcy i sprawdza zasadność profilu.
6. Nieudane testy i wymagane uwagi recenzenta kierują pracę do poprawy, z zachowaniem limitu rund. Poprawki podlegają świeżej weryfikacji i niezależnemu review.
7. Gotowość wymaga aktualnych dowodów dla tych samych plików. Końcowa akceptacja pozostaje osobną decyzją człowieka.

Raport podsumowujący wcześniejszą implementację otrzymuje od demona ograniczony zestaw dowodów: identyfikatory i daty przebiegów, wersje plików, kody zakończenia kontroli, liczbowe podsumowania testów, wyniki review i decyzje użytkownika. Historia pozostaje oznaczona jako historia; nie staje się świeżym dowodem dla nowego zadania. Brak rozpoznanego podsumowania testów jest jawny. Do tego zestawu nie trafiają wartości środowiska, surowe logi ani polecenia projektu.

Wykryty skrypt testowy musi rzeczywiście uruchamiać rozpoznany test i przekazywać jego niepowodzenie. Konstrukcje pomijające test albo maskujące błąd nie wystarczają. Wąski skonfigurowany test zachowuje pełne wykryte kontrole; szeroki test tego samego narzędzia usuwa zbędny duplikat. Niestandardowy skonfigurowany wrapper pozostaje jawną deklaracją zakresu projektu.

Automatyczne wykrywanie skryptów Composer analizuje ich treść, również tablice poleceń i odwołania do innych skryptów. Jako test rozpoznaje rzeczywiste uruchomienie PHPUnit lub Pest, także przez PHP. Samo `echo`, callback PHP, nieznany wrapper, cykliczny alias, maskowanie błędu oraz tryb pomocy, listowania czy inicjalizacji nie spełniają tego warunku. Zwykłe filtry wykonania Pest, np. `--todos` i `--issue`, nadal są testami. Nierozpoznany własny runner wymaga jawnego wpisu TestCommands; wykrywanie nie wykonuje skryptów podczas planowania.

## Kontrakt kontroli jednego przebiegu

Wąskie zadanie, np. niezależny katalog `workflow-demo/`, może mieć jawny kontrakt kontroli przypisany wyłącznie do istniejącego automatycznego przebiegu. Kontrakt zapisuje katalog, rzeczywiste komendy z jego manifestu, uzasadnienie i identyfikator żądania. Odkrywanie kontroli odczytuje manifesty wyłącznie tego obsługiwanego bezpośredniego podkatalogu: błędny manifest poza nim nie blokuje kontraktu, ale błąd w wybranym pakiecie nadal jest odrzucany. Panel Reviews i historia pokazują ten ograniczony zakres; nie oznacza on zaliczenia wszystkich kontroli projektu. Konfiguracja projektu oraz inne zadania pozostają bez zmian.

Kontrakt można wybrać przy jawnym Resume tylko dla zatrzymanego lub zablokowanego przebiegu, po potwierdzonym zakończeniu wcześniejszych operacji. Istniejące blokady niepewnego procesu nadal uniemożliwiają wznowienie. Żądanie `POST /api/v1/sessions/{sessionId}/local-workflow/resume` zawiera `expectedVersion` oraz opcjonalny obiekt `verificationScope` z polami `workflowId`, `requestId`, `scopePath` i `basis`. Identyfikator przebiegu i wersja muszą odpowiadać świeżemu odczytowi. Klient nie przekazuje wykonywalnych komend: serwer odkrywa je z manifestu katalogu, wymaga rzeczywistego testu i zachowuje wszystkie rozpoznane kontrole statyczne tego zakresu. Nieprawidłowe ścieżki, dowiązania i próba wybrania pustego sprawdzenia są odrzucane.

Resume tworzy świeży plan implementacji, testy i niezależne review. Model nie może zmienić profilu na raport bez testów ani wybrać kontroli spoza kontraktu. Zmiana zestawu odkrytych komend lub utrata rozpoznanego testu wymaga ponownego jawnego wyboru zakresu; zmienione pliki nadal podlegają świeżym testom i review. Kontrakt przetrwa ponowienie i restart tego samego przebiegu, ale nie przechodzi do kolejnego zadania użytkownika. Poprzednie wyniki i akceptacje pozostają historyczne; nowy wynik wymaga osobnej decyzji użytkownika.

## Obciążenie i widok tablicy

Tablica pobiera zbiorcze, ograniczone podsumowania zamiast pełnej historii każdej karty. Szczegóły i końcowa decyzja odczytują pełne dowody. Oznaczenie aktualności na karcie wymaga ciągłej obserwacji plików, bezczynnego wykonawcy i nadal ważnego właściwego review; po restarcie lub utracie dowodów jest zachowawcze. Podgląd wymaganej poprawki ma limit 1024 znaków, a pełna treść jest dostępna w szczegółach.

Planer odczytuje odpowiedź z ostatniej tury, zamiast ładować całą rozmowę. Bramka review sprawdza wersję plików jednym świeżym pełnym odczytem, bez powtarzania tego samego haszowania. Tanie sprawdzenie obserwacji używane do wyświetlania karty nie zastępuje pełnej kontroli przed akceptacją.

W jednym katalogu danych aplikacji mogą pracować najwyżej dwa hosty ciężkich kontroli projektu; kolejne czekają i można je anulować przed wykonaniem poleceń. Limit obowiązuje również pomiędzy instancjami runnera. Awaria hosta bez trwałego wyniku pozostawia jego miejsce zarezerwowane, ponieważ procesy testowe mogą nadal działać. Gdy nie ma innego bezpiecznego miejsca, workflow pokazuje jawną niepewność przydziału zamiast uruchamiać dodatkowy proces. Odzyskanie takiej rezerwacji wymaga osobnej interwencji z potwierdzeniem zakończenia poprzednich poleceń; Stop i restart nie usuwają jej automatycznie.

## Kolejne polecenia, restart i blokady

Nowe polecenie natychmiast unieważnia wcześniejszą gotowość. Podczas testów/review wiadomość Chat jest zachowana w kolejce. Demon kończy wyłącznie operacje poprzedniego workflow, następnie sam wznawia dostarczenie wiadomości. Wcześniejsze przebiegi pozostają w historii. Nie przerywa nowej pracy użytkownika jako skutku anulowania starego review.

Restart zachowuje intencje i historię. Rozpoczęte operacje są rekoncyliowane; po utracie obserwacji znane zakończone kontrole wykonuje się ponownie dla świeżych dowodów. Niepewny stary proces jest jawną blokadą, ponieważ nie wolno uruchamiać drugiego egzemplarza na podstawie przypuszczenia. Stop użytkownika pozostaje skuteczny; nowe jawne polecenie jest nową intencją.

Dotyczy to również polecenia testowego, którego proces główny zakończył się, lecz proces potomny nadal może pracować lub trzyma otwarty strumień wyjścia. Trwała niepewność drzewa polecenia pozostaje stanem `uncertain`, także po publikacji wyników hosta. Automatyczna poprawka, Resume i zastąpienie przebiegu nie mogą wtedy rozpocząć nowej pracy w tym katalogu. Sam kod zakończenia procesu głównego, upływ WaitDelay ani ponowne odczytanie wyniku nie potwierdzają zakończenia potomków.

Brak testów możliwych do ustalenia, konfiguracji recenzenta, dostępu do agenta lub potrzebnych informacji jest widoczną blokadą. Uzupełnienie komend w ustawieniach projektu automatycznie ponawia plan zatrzymany z powodu braku konfiguracji. Lista kontroli rozpoczętego przebiegu pozostaje zapisana w jego historii; nowe zwykłe polecenie tworzy kolejny przebieg z bieżącą konfiguracją. Rozpoznane przejściowe błędy dostawcy są obsługiwane automatycznie zgodnie z regułami OB-9 poniżej. Retry/Resume pozostają narzędziami naprawczymi dla pozostałych błędów. Surowy terminal nie ma trwałej kolejki semantycznych wiadomości, dlatego zachowuje blokadę wejścia podczas weryfikacji; pełna obsługa kolejki dotyczy Chat.

Aktualizacja nie uruchamia masowo historycznych rozmów. Istniejąca rozmowa zostaje objęta automatyzacją po nowym jawnym poleceniu. Ręczny Start pozostaje dla starych sesji oraz diagnostyki.

## Weryfikacja

Testy integracyjne używają rzeczywistego SQLite oraz podstawionych granic agentów. Obejmują pochodzenie wiadomości, provisioning, deduplikację, zmianę zakresu, plan code/report, naprawę testów, niezależne review, reset po restarcie, Stop, odzyskanie kolejki i świeżą akceptację. Kontrolowane testy renderera sprawdzają prezentację stanów i przejścia kart. Próba z rzeczywistymi modelami jest osobnym rodzajem dowodu i nie jest implikowana wynikiem testów z podstawionymi agentami.

Bieżące logi odbioru: `.superpowers/sdd/2026-10-19-automatic-workflow/` (lokalne, nie są częścią dystrybucji).

Kontrole i review dotyczą tej samej ocenianej wersji plików. Na macOS z systemem plików APFS sam odczyt pliku przez Git może zgłosić zdarzenie metadanych bez zmiany treści ani uprawnień. Taki sygnał jest pomijany wyłącznie dla zwykłego pliku o niezmienionej, wiarygodnej obserwacji tożsamości i metadanych. Rzeczywiste zmiany uprawnień, zmiana i przywrócenie pliku, zapis, nieznana ścieżka lub błąd odczytu nadal unieważniają wynik. Brak wiarygodnych metadanych nie uprawnia do pominięcia kontroli.

## Obserwacja zadania demonstracyjnego

W projekcie Trains Watch zadanie „DEMO workflow — implementacja etykiet etapów i testów unittest” dodaje tylko niezależny katalog `workflow-demo/`. Nie jest on podłączony do aplikacji. Użytkownik zatwierdził dla tej jednej próby osobny worktree; zadanie nie wykonuje commitów, pusha ani publikacji.

Po utworzeniu zadania otwórz tablicę projektu. Karta powinna przejść od wykonania do testów, następnie niezależnego review i oczekiwania na akceptację. Poprawki są potrzebne tylko po nieudanych testach lub wymaganych uwagach review; poprawny wynik może ominąć tę kolumnę. Dwa szybkie sprawdzenia demonstracji mogą zakończyć się przed kolejnym odświeżeniem tablicy. Ich rzeczywisty przebieg, komendy i wyniki pozostają w panelu zadania, w zakładce Reviews i historii rund.

Końcowa kolumna Accepted wymaga jawnej decyzji użytkownika. Nie trzeba naciskać Start, Resume ani Run local review, aby rozpocząć zwykły nowy cykl. Komunikat blokady oznacza nieukończoną próbę; nie jest równoważny zaakceptowaniu zadania.


## Odzyskiwanie dostępu do dostawcy — OB-9
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-9). -->

Automatyczny przebieg ponawia bez dodatkowego przycisku rozpoznaną awarię uwierzytelnienia, limit dostawcy lub przejściową niedostępność. Dowodem jest strukturalny błąd natywnego API albo świeża diagnostyka dostawcy, nigdy tekst odpowiedzi modelu, raportu review czy komunikat z terminala. Obsługiwane obserwacje obejmują natywne unauthorized/quota i HTTP 401/429/502/503/504; diagnostyka Claude obejmuje również rozpoznany timeout próby dostępu. Nieznany błąd, HTTP 403, wadliwy plan, brak konfiguracji, uwagi review i wyczerpanie rund pozostają oddzielnymi blokadami.

Jeden przebieg ma wspólny dla planowania i recenzenta limit trzech automatycznych prób. Następne opóźnienia wynoszą 30, 60 i 120 sekund. Licznik i termin są zapisane przed wywołaniem dostawcy, więc restart i odświeżanie tablicy nie zerują budżetu. Oczekiwanie nie wykonuje dodatkowych zapytań do modeli. Karta i panel pokazują powód, etap, liczbę prób oraz dokładny termin następnej próby; po powodzeniu informacja pozostaje historyczna. Po wyczerpaniu limitu trzeba przywrócić dostęp i jawnie użyć Resume; nie zwiększa to limitu rund poprawek.

Planer ponawia właściwe, ograniczone czasowo wywołanie planowania po potwierdzonym zakończeniu poprzedniego. Tymczasowy natywny wątek Codex ma dostęp tylko do odczytu, wyłączoną sieć narzędzi, powłokę, aplikacje, pluginy i wszystkie odziedziczone serwery MCP. Odczyt efektywnej konfiguracji jest wymagany przed uruchomieniem takiego wątku. Ustawienia zwykłych rozmów pozostają niezależne.

Odzyskiwanie recenzenta używa świeżej diagnostyki dla właściwego katalogu i środowiska projektu. Powrót oznacza nowe testy i nowe niezależne review dla aktualnych plików; poprzednia zgoda nie staje się aktualnym dowodem. Zmiana ustawień lub obowiązkowych kontroli wymusza świeży plan. Lokalny stan „zalogowany”, odnawialny token albo nieobsługiwany dostawca niestandardowy same nie potwierdzają dostępu. Zwykłe uruchomienie Claude zachowuje natywną obsługę odnowienia tokenu. Brak wiarygodnej diagnozy pozostawia jawną blokadę.

Automatyczne ponowienie wymaga pewnego zakończenia poprzedniego wykonania. Niepowodzenie zakończenia procesu, utrata trwałego wyniku albo restart w trakcie planowania pozostawia niepewność; nie uruchamiamy drugiego egzemplarza. Stop i nowe polecenie nie usuwają takiej informacji. Przy niepotwierdzonym zakończeniu własnego procesu należy zakończyć dotkniętą sesję i potwierdzić stan procesu przed nową pracą. Sama zmiana statusu review na failed albo wysłanie przerwania nie jest dowodem zamknięcia procesu.

Stop anuluje oczekiwanie i własne aktywne próby; spóźniona odpowiedź nie wznawia zatrzymanego przebiegu. Końcowa akceptacja pozostaje wyłącznie decyzją użytkownika. Testy z podstawionymi granicami agentów obejmują budżet, opóźnienia, restart, wyścigi, zmiany konfiguracji/plików, Stop oraz niepewne zakończenie. Nie stanowią deklaracji przeprowadzenia rzeczywistej awarii konta produkcyjnego.
