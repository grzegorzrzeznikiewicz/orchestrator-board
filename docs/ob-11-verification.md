# OB-11 — weryfikacja CI

Zmiany przygotowano na `feature/OB-11`, od `origin/main` (`df4cabc`).
Raport dotyczy lokalnej weryfikacji przed publikacją zmian.
W tym etapie nie wykonywano commita, pusha ani zmian reguł na GitHubie.
Weryfikacja lokalna: macOS arm64, Go 1.27.1, Node.js 24.22.0 i Docker Desktop.

## Wdrożony zakres

- Główne CI dla każdego PR do `main`, pushu do `main` i uruchomienia ręcznego.
- Dobór istniejących workflow do zmienionych obszarów z uwzględnieniem wspólnych
  pakietów, kontraktów i zależności obu modułów Go w `go.work`.
- Jeden wynik `CI / required`, odrzucający błędy, anulowania, brakujące wyniki
  i nieuzasadnione pominięcia. PR dokumentacyjny otrzymuje końcowy wynik.
- ESLint frontendu, TypeScript/React Hooks oraz jawna baza 144 wcześniejszych
  naruszeń w 73 plikach. CI nie rozszerza tej bazy.
- Walidacja workflow, kontrola wygenerowanych plików API/SQL, raporty Vitest
  i Playwright oraz zachowanie trace i screenshotów po błędach.
- Dwa procesy Playwright i jedna ponowna próba w CI; ciężkie pakowanie Electron
  pozostaje osobnym workflow uruchamianym ręcznie.

## Potwierdzone lokalnie

| Kontrola | Wynik |
| --- | --- |
| Testy doboru kontroli i bramki CI | 67/67 |
| ESLint całego skonfigurowanego zakresu | 0 nowych błędów i ostrzeżeń z przyjętą bazą |
| Testy rzeczywistego działania reguł ESLint i bazy wyjątków | 6/6 |
| actionlint 1.7.12 — wszystkie workflow | Poprawne |
| Formatowanie Go, kompilacja, vet i golangci-lint backendu | Poprawne; golangci-lint: 0 issues |
| Pełny backend `go test -race -count=1 -timeout=20m -p 2 ./...` | 200 pakietów przeszło, 0 błędów; 14 pakietów bez testów |
| Regeneracja OpenAPI, typów TypeScript i sqlc | Bez różnic i nowych nieśledzonych plików |
| TypeScript frontendu i zestawu E2E pod Node 24 | Obie pełne kontrole przeszły |
| Renderer smoke po poprawce zaznaczania tekstu | 83/83, bez ponowień; dwa procesy, konfiguracja CI |
| Powtórzenia testów kursora i zaznaczania tekstu | 20/20, bez ponowień |
| Pełny Vitest frontendu z natywnym agent-browser | 474 pliki; 8027 testów przeszło, 6 pominięto |
| Pakiet cloud-client | Kontrola kontraktu, typy, build i 29 testów przeszły |
| Pakiet product-ui | Typy, build i 160 testów przeszły |
| Aplikacja mobilna | Typy i 1908 testów w 214 plikach przeszły |
| Moduł Cloud | Pełne testy z `-race`, kompilacja i vet przeszły |
| Expat security backport | Build docelowego etapu Docker przeszedł |
| Natywne CLI E2E na macOS | Przeszły |
| Świeża instalacja CLI w Dockerze | `fresh-install check: OK` |
| Pomocnik aktualizacji macOS | 13 testów, kompilacja x64/arm64, kontrola architektur i testy stanu Swift przeszły |
| Kontrakt runnera pod E2E | 17/17 |
| Zgodność lockfile frontendu | `npm ci --ignore-scripts --dry-run` przeszło |

## Poprawka E2E i ograniczenia środowiska

Pierwszy pełny renderer smoke z dwoma procesami zakończył się wynikiem 82/83.
Istniejący błąd w `e2e/chat-composer-text-selection.spec.ts` odtworzono także
z konfiguracją Playwright z `HEAD`, a następnie w samodzielnym przykładzie
przeglądarkowym bez kodu aplikacji. Dla kroju Geist punkt o jeden piksel przed
prawą krawędzią końcowej kropki przesuwał kursor zamiast tworzyć zaznaczenie.

Test czeka teraz na `document.fonts.ready` przed pomiarem tekstu i zaczyna
przeciągnięcie dwa piksele za ostatnim znakiem. Zachowano rzeczywisty gest
myszą oraz wszystkie asercje zakresu i kontrastu w 18 wariantach motywu.
Oba scenariusze z pliku powtórzono po 10 razy: **20/20 przeszło bez ponowień**.
Kontrola typów E2E, pełny lint frontendu i 6 testów reguł ESLint również przeszły.
Niezależny przegląd poprawki nie wykazał usterek.

Końcowy pełny renderer smoke z konfiguracją CI: **83/83, bez ponowień**.
W poprzednim przebiegu po poprawce zaznaczanie tekstu przeszło, ale przeładowanie
strony przerwało scenariusz klonowania repozytorium (82/83). Trace zachowano;
w końcowym przebiegu ten scenariusz także przeszedł za pierwszym razem.
Logi: `.cache/ob11-selection-fixed.log`, `.cache/ob11-smoke-selection-fixed.log`
i `.cache/ob11-smoke-selection-final.log`.


`test:browser-postcondition-integration` nie przechodzi w tym środowisku macOS:
Electron zgłasza `Current display surface not available for capture`.
Workflow zachowuje ten test na Linuxie, gdzie skrypt uruchamia go przez Xvfb;
wynik tego środowiska trzeba potwierdzić w CI.

Pierwszy lokalny przebieg Vitest używał systemowego Node 25 i niezgodnego ABI
modułu SQLite. Końcowy pełny przebieg pod Node 24, zgodnym z CI, przeszedł.

## Przegląd i dalsza aktywacja

Niezależny przegląd wskazał brak wyboru Cloud dla publicznych pakietów backendu
i zmian zależności Go. Dodano testy, potwierdzono ich niepowodzenie przed poprawką,
a następnie poprawiono selektor; cały zestaw 67 testów przechodzi.
Przegląd nie wskazał innych usterek krytycznych, ważnych ani drobnych.

Nie wykonywano zdalnych workflow, skanera Gitleaks na runnerze GitHub ani
testów Windows/macOS Intel. Nie oznaczono ich jako zaliczonych. Zgodnie z zakresem
nie uruchamiano publikacji ani pełnego pakowania aplikacji.

Po uzyskaniu zgody na push wymagane są rzeczywiste przebiegi Actions i ustawienie
końcowego sprawdzenia jako wymaganego przed merge do `main`. Procedurę opisuje
[ci.md](ci.md). Do potwierdzenia testu przechwytywania obrazu Electron na Linuxie
i rzeczywistych przebiegów Actions nie ma podstaw do stwierdzenia, że całe CI
jest zielone. Blokada merge pozostaje nieaktywna do skonfigurowania reguły GitHub.

Pełne logi lokalnych przebiegów znajdują się w ignorowanym katalogu `.cache/ob11-*`.

## Pierwsza weryfikacja na GitHubie

Po zgodzie na publikację utworzono PR #2. Pierwszy przebieg wykrył brak wpisu
opcjonalnej zależności peer `@pierre/theme@1.1.0` pod `@pierre/trees` w lockfile.
Odtworzono błąd dokładnie na Node 22.23.2 / npm 10.9.8, używanych przez kontrolę
zgodności. Regeneracja samego lockfile tą wersją npm dodała 11 linii bez zmiany
wersji istniejących zależności. Kontrola `npm ci --ignore-scripts --dry-run`
przeszła następnie zarówno pod Node 22 / npm 10, jak i pod Node 24 / npm 11.
Pełny wynik aktualnego commita należy sprawdzić w Actions przypisanych do PR.
