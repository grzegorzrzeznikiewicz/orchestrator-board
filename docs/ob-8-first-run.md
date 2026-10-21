# OB-8 — pierwsze uruchomienie macOS i workflow Local
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-8). -->

Instrukcja prowadzi od instalacji Orchestrator Board do sprawdzenia zadania i jego akceptacji przez użytkownika. Dotyczy lokalnego pakietu macOS arm64 z wersją aplikacji `0.13.0`, sprawdzonego 20 października 2026. Pochodzenie źródeł i zakres wykonanej próby podano na końcu. Dokument nie oznacza zamknięcia epika ani zakończenia audytu licencji OB-10.

## 1. Zainstaluj właściwą aplikację

Uzgodniona dystrybucja to niepodpisany DMG do osobistej instalacji i ręcznej wymiany aplikacji. Użytkownik nie ma jeszcze konfiguracji Apple Developer ID. Podpisywanie, notaryzacja i podpisane automatyczne aktualizacje pozostają poza tym zakresem.

1. Użyj sprawdzonego lokalnego DMG albo jawnie opisanego niepodpisanego wydania, gdy zostanie udostępnione w [GitHub Releases tego forka](https://github.com/grzegorzrzeznikiewicz/orchestrator-board/releases). Porównaj architekturę, wersję i SHA-256 z informacją konkretnego artefaktu. Ten przewodnik nie zakłada, że plik został już opublikowany, i nie podaje domyślonego adresu pobrania.
2. Przy wymianie istniejącej kopii zakończ ją przez menu ikony na pasku macOS: **Quit Orchestrator Board Completely**. Zachowaj poprzednią kopię i kopię danych. Otwórz DMG i skopiuj **Orchestrator Board.app** do **Applications**. Zachowaj osobną **Agent Orchestrator.app**, jeżeli jest zainstalowana.
3. Otwórz `/Applications/Orchestrator Board.app` przez Finder. Niepodpisana aplikacja może zostać zatrzymana przez Gatekeeper; pakiet nie ma potwierdzenia zaufania Apple. Nie wyłączaj zabezpieczeń systemu. Jeśli macOS blokuje start, postępuj według jego procedury dla świadomie wybranej aplikacji.
4. W menu aplikacji otwórz **About Orchestrator Board**. Sprawdź nazwę, wersję i pochodzenie od Agent Orchestrator. **Licenses** udostępnia sześć zasobów: `LICENSE`, `NOTICE`, `THIRD-PARTY-LICENSES.txt`, `ASSET-NOTICES.txt`, `Electron-LICENSE.txt` i `Chromium-LICENSES.html`.

Wydania i aktualizacje muszą pochodzić z repozytorium forka. Brak wydania lub kanału pozostaje błędem dostępności. Ręczna instalacja DMG nie jest testem automatycznej aktualizacji. Szczegóły opisuje [procedura wydania desktopowego](../frontend/docs/desktop-release.md).

## 2. Zachowaj właściwy katalog danych

Dane demona, projekty i trwałe ustawienia korzystają z `~/.ao` albo jawnie wybranego `AO_DATA_DIR`. Profil okna forka jest osobny: domyślnie `~/.ao/orchestrator-board/electron`, a przy jawnym `AO_DATA_DIR` — `<AO_DATA_DIR>/orchestrator-board/electron`. Oryginalne `~/.ao/electron` nie jest automatycznie otwierane, kopiowane ani migrowane. Preferencje okna i sesje przeglądarki zaczynają osobno; istniejące projekty pozostają w wybranym katalogu danych demona. Dane logowania do chmury forka mają własną nazwę i nie przejmują plików oryginalnej aplikacji.

Instalacja korzystająca ze specjalnie wybranego katalogu może odczytać jego wybór z opcjonalnego `~/.ao/orchestrator-board/desktop-launch.json`. Zwykły start z Findera/Docka korzysta z tego pliku. Aplikacja nie zapisuje tam automatycznie zmiennych użytych przy próbie z terminala. Poprawny plik wymaga wersji `1`, pól `dataDir`, `runFile`, `port` i bezwzględnych ścieżek wewnątrz `~/.ao`; błędny wybór zatrzymuje start zamiast otworzyć inną bazę. Jawny `AO_DATA_DIR` omija zapisany wybór.

Jeśli projekty pozornie zniknęły, najpierw sprawdź, czy aplikacja korzysta z wcześniejszego katalogu danych. Pełny kontrakt, w tym pierwszeństwo zmiennych środowiska, opisuje [dokument tożsamości i danych](ob-10-fork-identity.md#data-and-coexistence).

## 3. Przygotuj Codex i Claude Code

Workflow Local wymaga **Codex** jako wykonawcy oraz dokładnie jednego recenzenta **Claude Code**. Orchestrator Board korzysta z lokalnych narzędzi i ich uwierzytelnienia. Logowanie do chmury aplikacji nie zastępuje dostępu do tych dwóch dostawców.

1. Otwórz **Settings → Harness**. Wybierz **Local** i **This computer**, jeżeli te przełączniki są widoczne. Odszukaj osobno **Codex** i **Claude Code**.
2. Przy brakującym narzędziu użyj oferowanej akcji **Install** albo **Open installation guide**. Dla Codex źródłem jest [oficjalna instrukcja instalacji CLI](https://learn.chatgpt.com/docs/codex/cli). Aplikacja musi móc odnaleźć lokalne programy `codex` i `claude`.
3. Jeśli potrzebne jest logowanie, użyj **Login** przy właściwym narzędziu i dokończ jego procedurę w otwartym terminalu lub przeglądarce. Codex można też zalogować natywnie przez `codex login`, a stan sprawdzić przez `codex login status`, zgodnie z [oficjalną dokumentacją uwierzytelniania](https://learn.chatgpt.com/docs/auth). Claude Code korzysta z własnej procedury; postępuj według komunikatów tej instalacji. Nie wpisuj poświadczeń do treści zadania.
4. Wróć do ustawień i sprawdź odświeżony stan instalacji i logowania obu narzędzi. Szczegóły mają zakładki **Account**, **Models** i **Health**. Wybieraj model dostępny dla danego konta; nie kopiuj nieaktualnej nazwy modelu z wcześniejszego raportu.

Lokalny stan logowania pomaga w konfiguracji, ale sam nie dowodzi dostępnego limitu lub udanego wywołania modelu. Błąd dostawcy w rzeczywistym przebiegu pozostaje widoczny. Integracje opisują też lokalne przewodniki [Codex](../frontend/src/docs/content/plugins/agents/codex.mdx) i [Claude Code](../frontend/src/docs/content/plugins/agents/claude-code.mdx).

## 4. Dodaj lokalny projekt

1. Wybierz **Add project** i wskaż katalog główny istniejącego repozytorium. Użyj projektu, w którym możesz wykonywać uzgodnione zmiany i uruchamiać testy.
2. W formularzu agentów pozostaw **Review mode → Local workflow**. Wykonawca jest ustawiony na **Codex**. Wybierz też dostępnego agenta **Orchestrator**, który prowadzi rozmowę na poziomie projektu.
3. Wybierz **Create and start**. Rejestracja otwiera tablicę projektu i automatycznie uruchamia orkiestratora w tle. Jeśli nadal jesteś na tej tablicy, aplikacja przechodzi do jego rozmowy po uruchomieniu.
4. Jeśli orkiestrator nie wystartuje, projekt pozostaje dodany, a aplikacja pokazuje błąd. Uzupełnij wskazaną konfigurację zamiast ponownie dodawać ten sam katalog.

Zwykłe nowe zadanie w projekcie Git otrzymuje własną gałąź i worktree. Sam wybór Local nie zmienia tej zasady. Ustal zasady pracy dla projektu przed utworzeniem zadania. W odbiorze opisanym na końcu wykorzystano istniejące zadanie zaakceptowane przez użytkownika; przygotowanie instrukcji nie tworzy kolejnego worktree ani zadania.

## 5. Ustaw komendy testowe projektu

Przed wysłaniem pierwszego zadania implementacyjnego otwórz ustawienia projektu, następnie **General → Review workflow**.

1. Potwierdź **Review mode → Local review**. Formularz tworzenia projektu używa etykiety **Local workflow**, a ustawienia — **Local review**; oba odnoszą się do lokalnego procesu weryfikacji.
2. W **Test commands** wpisz jedną istniejącą, sprawdzoną komendę na linię. Każda komenda zaczyna pracę od katalogu zadania. Zmiana katalogu w jednej linii nie przechodzi do kolejnej, więc komendy dla podkatalogu muszą zawierać własne `cd`.
3. Ustaw **Maximum workflow rounds**, domyślnie `3`. W ustawieniach ról potwierdź **Worker: Codex** i **Reviewer: Claude Code** oraz dostępne modele. Zmiana agenta orkiestratora może uruchomić jego zastępczą sesję.
4. Poczekaj na zakończenie zapisywania. Otwórz ustawienia ponownie i potwierdź zachowane komendy i limit. Zależności i środowisko testów muszą być dostępne dla uruchomionej aplikacji desktopowej.

Przykład dla repozytorium, które rzeczywiście ma te dwa zestawy testów:

```text
cd backend && go test ./...
cd frontend && npm test
```

Użyj komend swojego projektu. Same lint, typecheck, vet lub polecenie, które nic nie testuje, nie zastępują testów implementacji. Zapisane komendy są obowiązkowym minimum; planer może dołączyć istniejące kontrole wykryte w manifestach projektu. Wąska komenda nie oznacza automatycznie pominięcia szerszych wykrytych kontroli. Brak możliwego do ustalenia testu jest blokadą wymagającą wyjaśnienia w konfiguracji.

## 6. Wyślij zadanie i obserwuj przebieg

Utwórz **New task** w projekcie, wybierz **Chat** i opisz wynik, zakres plików oraz kryteria odbioru. Jeżeli praca ma pozostać lokalna, zapisz w poleceniu zakaz commitów, push i tworzenia PR. Wyślij zadanie po przygotowaniu komend i dostępu obu agentów.

Wysłanie polecenia automatycznie rozpoczyna proces. Zwykłe nowe zadanie i kolejne jawne polecenie w istniejącej rozmowie nie wymagają **Start workflow**, **Resume workflow** ani **Run local review**. Ręczny Start pozostaje dla starszych sesji i diagnostyki.

| Etap na tablicy | Co sprawdzić |
| --- | --- |
| **Execution** | Codex wykonuje polecenie; aplikacja czeka na zakończenie pracy. |
| **Tests** | Panel pokazuje plan weryfikacji, komendy i rzeczywiste wyniki. |
| **Local review** | Claude Code niezależnie sprawdza ocenianą wersję plików lub odpowiedź. |
| **Corrections** | Wymagane uwagi lub nieudane testy wracają do Codex; potem następują świeże testy i review. |
| **Awaiting acceptance** | Aktualne kontrole przeszły, review nie ma wymaganych poprawek; decyzję podejmuje użytkownik. |
| **Accepted** | Użytkownik zaakceptował aktualnie oceniony wynik w aplikacji. |

Poprawny przebieg może ominąć **Corrections**. Krótki etap może zakończyć się między odświeżeniami tablicy; panel **Local workflow** i historia rund zachowują dowody. **Show stdout** i **Show stderr** otwierają wyniki konkretnej komendy.

Implementacja otrzymuje profil **Code changes** i wymaga rzeczywistych testów. Raport może otrzymać profil **Report**: recenzent kontroluje treść, zakres, źródła i fakty, a brak testów kodu jest oznaczony jako nie dotyczy. Plan i uzasadnienie są widoczne. Raport o dawnej implementacji może korzystać z oznaczonej historii; nie zamienia jej w nowe wyniki testów.

## 7. Podejmij decyzję i rozpoznaj blokadę

W **Awaiting acceptance** otwórz zadanie i sprawdź bieżący plan, wyniki i uwagi review. Wybierz **Accept changes** i potwierdź natywny dialog, jeśli wynik spełnia wymagania. Wymagane zmiany odeślij przez **Request changes** z komentarzem. Końcową decyzję podejmuje użytkownik; odpowiedź modelu ani zielony wynik testów nie zastępują akceptacji.

Zmiana plików, nowe polecenie lub utrata obserwacji unieważnia wcześniejszą gotowość. Podczas testów/review kolejne polecenie Chat pozostaje w kolejce; po bezpiecznym zakończeniu poprzednich operacji aplikacja dostarcza je sama. Nie wysyłaj duplikatu tylko dlatego, że wiadomość oczekuje. Surowy Terminal UI nie ma takiej samej trwałej kolejki i blokuje wejście podczas weryfikacji.

| Limit lub blokada | Znaczenie i dalszy krok |
| --- | --- |
| **Maximum workflow rounds: 3** | Limit cykli testy/review/poprawki danego przebiegu. Po wyczerpaniu obejrzyj wymagane poprawki; zwiększenie limitu i wznowienie wymaga jawnej decyzji. |
| Trzy automatyczne próby dostawcy | Osobny budżet OB-9, wspólny dla planera i recenzenta: opóźnienia 30, 60 i 120 sekund. Oczekiwanie pokazuje przyczynę, etap, licznik i termin. Nie zużywa dodatkowej rundy poprawek ani nie wymaga naciskania Resume. |
| Wyczerpany budżet dostawcy | Przywróć dostęp lub limit u dostawcy, następnie jawnie użyj **Resume workflow**. Restart nie zeruje budżetu. |
| Brak konfiguracji | Uzupełnij wskazane ustawienia. Zapis komend projektu ponawia plan zatrzymany z powodu brakującej konfiguracji; pozostałe błędy mogą wymagać jawnego wznowienia. |
| Nieaktualne dowody | Wymagana jest świeża weryfikacja aktualnych plików. Dawna akceptacja zostaje historią. |
| Niepewne zakończenie procesu | Sam restart lub Stop nie dowodzi zakończenia poprzedniej pracy. Potwierdź stan dotkniętego procesu przed nową próbą; aplikacja nie uruchamia drugiego egzemplarza na podstawie przypuszczenia. |

**Stop workflow** zatrzymuje dalszy przebieg i anuluje oczekiwanie na automatyczną próbę. Błędy konfiguracji, uwagi review i nieznany błąd nie są automatycznie naprawionym dostępem do dostawcy. Pełne reguły opisuje [automatyczny workflow Local](automatic-local-workflow.md).

## 8. Sprawdź ponowne otwarcie

Wykonaj próbę po zakończeniu aktywnej pracy. Zapisz, który projekt, rozmowa i wynik mają zostać zachowane. Możesz wpisać niewysłany szkic wiadomości, aby sprawdzić jego odtworzenie bez zlecania kolejnej pracy.

1. Zakończ aplikację przez **Quit Orchestrator Board Completely** w menu ikony na pasku macOS. Zamknięcie okna lub zwykłe Cmd+Q może pozostawić aplikację w pasku.
2. Uruchom **Orchestrator Board.app** przez Finder albo Dock, bez powtarzania specjalnego polecenia z terminala.
3. Sprawdź ten sam katalog danych, listę projektów, ostatnio wybraną lokalną tablicę, motyw i szkic po otwarciu rozmowy. Jawny odsyłacz do zadania lub ustawień ma pierwszeństwo przed odtworzeniem tablicy.
4. Sprawdź historię workflow i decyzji. Po utracie obserwacji aplikacja może wymagać nowych testów i review dla bieżących plików. Zachowanie historii nie gwarantuje natychmiastowej aktualności dawnej akceptacji. Niepewna stara operacja pozostaje jawną blokadą.

Lista kontrolna pierwszej instalacji lub wymiany:

- [ ] Właściwa aplikacja i architektura, zapisany SHA-256 instalatora; oryginalna aplikacja zachowana.
- [ ] About i sześć zasobów licencji dostępne; źródło i wydania wskazują fork.
- [ ] Codex i Claude Code zainstalowane i gotowe do właściwych ról.
- [ ] Projekt Local, rzeczywiste komendy testowe i limit zapisane przed zadaniem.
- [ ] Widoczne wyniki wykonania, testów i niezależnego review; poprawki sprawdzone ponownie, jeśli wystąpiły.
- [ ] Jawna decyzja użytkownika dla aktualnego wyniku.
- [ ] Pełne zakończenie i zwykły start zachowują wybór danych, projekt, szkic, motyw i historię.
- [ ] Blokady, nieaktualność i ograniczenia zakresu zapisane jako takie; brak dowodu nie jest wynikiem pozytywnym.

## Pochodzenie źródeł i dowody z 20 października 2026

Wersja pakietu nie identyfikuje samodzielnie rewizji źródeł. Początkowo analizowane `v0.13.3` nie jest bazą obecnego lokalnego odbioru.

| Identyfikator | Ustalona wartość |
| --- | --- |
| Upstream tag obecny w historii | `v0.14.2`, SHA `55b2962a5f7478fa7570777aad92f3bef3c99ccd` |
| Baza forka po aktualizacji upstream | `df4cabc544dcbe2f0e4db5c8d6132ebb623a4d2b`, lokalne `git describe`: `v0.14.2-16-gdf4cabc` |
| HEAD pakietu z odbioru OB-9 | `f64ed0f6d1ee42f7395b15d5e7b63d6a9451e3e1` plus niezatwierdzone zmiany; `v0.14.2-25-gf64ed0f` |
| Wersja aplikacji | `0.13.0` z `frontend/package.json`; ta sama wartość była już w manifeście bazy upstream |
| Manifest źródeł pakietu OB-9 | 5235 wpisów, SHA-256 `bfce984aded884ed2c7d25c2f2c47b6354105b2d9219a06f15436d5c248c9355` |

Manifest dotyczy zainstalowanego artefaktu OB-9, przed dopisaniem instrukcji. Nie obejmuje automatycznie późniejszych zmian dokumentacji. Jej weryfikacja nie wymaga commita, push ani zmiany zainstalowanego pakietu.

Zapis odbioru natywnego `20261020T171621Z` potwierdza zgodność 7073 wpisów zainstalowanej aplikacji z wybranym artefaktem, działający demon na porcie 3010, zachowane projekty i wybór uruchomienia oraz motyw Light. Profil tej konkretnej instalacji to `~/.ao/dev/ob10/native-data/orchestrator-board/electron`; nie jest to domyślna ścieżka wszystkich użytkowników. SHA-256 zainstalowanego demona: `83dc15a040b89a69ce109f80a5b31fbfceacf5f66a51af56d53195da59365846`; `app.asar`: `c5b5143366d521affb52a9853d39ac62bcef8c3d60d6b17d33cc5149e672e745`.

Rzeczywiste istniejące zadanie w Trains Watch przeszło świeże testy i review Claude po ponownym uruchomieniu. Użytkownik zaakceptował wynik bezpośrednio w aplikacji `2026-10-20T17:23:21Z`. Końcowy odczyt ma `stage=accepted`, `accepted=true`, `current=true`, wersję przebiegu `36`, cykl `2/3` oraz snapshot `c6811aa5588740ee836c85a9562af5b29aeac8a0fd939df4bd0bf1442e9238c5`. Pięć kontroli zakończyło się poprawnie; historia zachowuje wcześniejszy nieudany test i późniejsze udane powtórzenia. Są to dowody konkretnej próby z rzeczywistymi agentami. Nie potwierdzają przeprowadzenia wszystkich scenariuszy awarii dostawcy ani ponownego wykonania całej pierwszej konfiguracji w obecnej instalacji.

Lokalne pliki dowodowe, niewchodzące do dystrybucji:

- `~/.ao/dev/ob9/native-acceptance/20261020T171621Z/acceptance.json`
- `~/.ao/dev/ob9/native-acceptance/20261020T171621Z/workflow-user-accepted-final.json`
- `~/.ao/dev/ob9/native-acceptance/20261020T171621Z/source-after-install.json`
- `.superpowers/sdd/2026-10-20-ob-9-provider-recovery/source-freeze.json`

Odczyt dowodów pozwala ocenić wykonaną próbę bez ponownego logowania do dostawców, wysyłania poleceń do zaakceptowanego zadania lub zmiany projektu. Nie zastępuje świeżego odbioru innej wersji pakietu. Stan podpisu Apple, przyszłe wydania, audyt zależności i pozostałe kryteria OB-10 opisuje [dokument tożsamości i licencji](ob-10-fork-identity.md#licenses). Zachowane Apache-2.0 i autorstwo upstream nie oznaczają automatycznego rozstrzygnięcia wszystkich licencji zależności.
