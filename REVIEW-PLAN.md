# Korjaussuunnitelma: katselmoinnin avoimet löydökset

Korvaa aiemman `REVIEW-TODO.md`:n. Jokainen löydös on varmistettu koodista
(kolme rinnakkaista analyysiä + omat tarkistukset) committia `63c9174` vasten.
**Katselmointiraportti oli osin väärässä** — ks. "Korjaukset katselmointiin".

Jo korjattu: `9d8fb58` (kuollut koodi), `63c9174` (reply-editori + kursori),
**`3d89998` (vaihe 1: hakukerros, löydökset 1 ja 7b)**,
**`077413b` (vaihe 2: render.go, löydökset 5 ja 8)**.

## Yhteenveto

| # | Löydös | Verdikti | Koko | Vaihe |
|---|--------|----------|------|-------|
| 1 | `includeHistory` fataali snippet-virhe | Vahvistettu, **diagnoosi väärä** | ~5 r | 1 ✅ |
| 2 | `commentLineRange` koordinaatistot | Vahvistettu | ~60 r + GraphQL | 4 |
| 3 | Listakorkeus 1 | Vahvistettu, **2 lisävikaa** | ~35 r | 3 |
| 4 | Kutistettu > laajennettu | Vahvistettu, **vaatii testimuutokset** | ~8 r | 3 |
| 5 | Kommentin body katoaa | Osittain vahvistettu | ~25 r | 2 ✅ |
| 6 | Status-suodatin hiljaa | Vahvistettu | ~12 r | 3 |
| 7a | `threadPreview` per frame | Vahvistettu, 195 µs/kutsu | ~25 r | 3 |
| 7b | Duplikaatti GraphQL-kutsu | Vahvistettu, **määrä liioiteltu** | ~40 r | 1 ✅ |
| 8 | `compactSnippetLines` tyhjät rivit | Vahvistettu | ~20 r | 2 ✅ |

---

## Korjaukset katselmointiin

Kolme kohtaa, joissa alkuperäinen raportti johtaisi väärään korjaukseen:

**1. Älä palauta ehdollista `includeHistory`ä.** Raportti ehdotti vaihtoehtona
paluuta muotoon `includeHistory := showDiff`. Se olisi regressio:
`render.go:95` kutsuu `printHistoricalSnippet`ia `if commentIdx == 0` -lohkossa
**`opts.ShowDiff`-ehdon ulkopuolella**, eli myös tavallinen summary näyttää
snippetit. `--show-diff` portittaa `DiffHunk`-lohkon, ei snippettiä. Commit
`3c7fc0a` teki oikein muuttaessaan tämän `true`ksi; se vain unohti pehmentää
virhepolun. **Korjaa vain virheenkäsittely, jätä `app.go:137` ennalleen.**

**2. Duplikaattikutsujen määrä oli liioiteltu.** `attachHistoricalSnippets:610`
memoisoi `cache[key] = lines` **myös nil-tuloksen**, joten duplikaatti on
2 kutsua per uniikki `(commit, path)` — ei 2 per kommentti. Pahempi puoli on
toinen: `gitremote.Cache` muistaa negatiivisen tuloksen, mutta
`fetchLocalOrRemote` ei luota siihen, joten **jokainen TUI:n refresh tekee
suoran kyselyn uudelleen ikuisesti**.

**3. Löydös 4 ei ole korjattavissa nykyisiä testejä rikkomatta.** Todistus:
`TestRenderDetailCollapsedShowsSelectedComment` (korkeus 5, kutistettu) vaatii
`collapsed(5) >= 3`; `TestRenderDetailExpandedRespectsHeightWindow` (korkeus 6)
vaatii `expanded(6) <= 2`. Korkeusmonotoniselle budjetille
`expanded(5) <= expanded(6) <= 2 < 3 <= collapsed(5)`. Kaksi testiväitettä on
löysättävä.

---

## Vaihejako

Vaiheet ovat riippumattomia ja committoitavissa erikseen. Järjestys on valittu
niin, että aiempi vaihe ei riko myöhempää.

- **Vaihe 1 — hakukerros** (löydökset 1, 7b). Pienin riski, suurin
  käyttäjävaikutus: poistaa komennon kaatumisen.
- **Vaihe 2 — render.go** (8, sitten 5). 8 ensin, koska 5:n korjaus nojaa
  `compactSnippetLines`in tyhjä-sopimukseen.
- **Vaihe 3 — TUI** (3, 4, 6, 7a). Suurin tiedostokohtainen muutos, oma commit
  per löydös.
- **Vaihe 4 — GraphQL-skeema** (2). Ainoa, joka koskee kolmea pakettia ja
  vaatii välimuistiversiopäätöksen. Viimeisenä.

Ennen vaihetta 3: Step 0 -siivous `view.go`:hon (löydöksen 3 korjaus poistaa
`buildThreadListLines`in `window`-parametrin ja `selectionLine`-paluuarvon).

---

# Vaihe 1 — hakukerros ✅ VALMIS (`3d89998`)

## 1.1 Snippet-virhe ei saa kaataa komentoa

**Juurisyy:** `internal/threads/service.go:450-453` palauttaa virheen, kun
`service.go:377-380` vain logittaa. Sama kutsu, epäsymmetrinen käsittely.

**Virheluokat** (`fetchLocalOrRemote:645-659` nielee jo paikallisen gitin ja
`remoteCache`in virheet; ainoa läpi pääsevä on `client.FileLines`):

| Luokka | Nyt | Pitäisi |
|--------|-----|---------|
| 404 / puuttuva blob | `(nil, nil)`, ei virhe | ok |
| Tyhjä blob | `(nil, nil)` | ok |
| Rate limit, SAML, pääsy estetty | **kaataa komennon** | varoitus |
| `gh`-prosessi kaatuu | **kaataa komennon** | varoitus |
| JSON-dekoodaus | **kaataa komennon** | varoitus |
| ctx peruttu | kaataa | ok (kaataa muutenkin) |

Yhtään aidosti fataalia luokkaa ei ole — snippetit ovat koristetta.

**Korjaus.** Tee `attachHistoricalSnippets`ista rakenteeltaan ei-kaatuva, jotta
epäsymmetria ei voi palata:

```go
// attachHistoricalSnippets liittää koodikontekstin kommentteihin. Snippetit
// ovat koristetta: haun epäonnistuminen ei ole virhe, vaan palautetaan
// epäonnistuneiden (commit, path) -parien määrä ja ensimmäinen virhe.
func (s *Service) attachHistoricalSnippets(ctx context.Context, ghCtx Context, threads []ReviewThread) (failed int, firstErr error)
```

Molemmat kutsupaikat (`:378`, `:451`) logittavat yhden koosteen ja jatkavat.

**Renderöijä kestää puuttuvan snippetin — varmistettu:** `render.go:299-300`
(nil-vartio), `render.go:85-86` (body tulostuu kun snippettiä ei ole),
`render.go:266-268`, `view.go:414`. Ei nil-deref-riskiä.

**Varoituksen näkyvyys — huomioi.** `s.logf` kirjoittaa `s.logWriter`iin, joka
on `app.go:135` kautta `os.Stderr`. Interaktiivisessa tilassa `Refresh`
(`app.go:156-166`) ajaa `FetchData`n **käynnissä olevan TUI:n sisältä** ja
`tea.WithAltScreen()` omistaa ruudun → raakateksti stderriin sotkee
renderöinnin. Aseta `interactive.ProgramConfig`in refresh-polulle
`io.Discard` tai ohjaa viesti TUI:n statusriville.

Yksi kooste (`"Warning: could not load code context for 3 of 12 files (rate
limit exceeded)."`) on parempi kuin rivi per tiedosto — rate limit tuottaisi
muuten rivin jokaisesta.

**Testit** (`internal/threads/service_test.go`; laajenna `fakeClient`iä
kentillä `fileLinesErr` ja `fileLinesCalls`):
- `TestFetchReviewThreadsSurvivesSnippetFailure` — `fileLinesErr` asetettu →
  `err == nil`, thread palautuu, `Snippet == nil`, lokissa `"Warning"`.
- `TestFetchDataSnippetFailureIsNotFatalOnFreshFetch`
- `TestAttachHistoricalSnippetsSkipsMissingBlobWithoutError`
- `internal/render/render_test.go`: `TestPrintSummaryRendersBodyWhenSnippetMissing`
  — lukitsee `render.go:85-86` -fallbackin, jonka varaan korjaus nojaa.

**Riski:** `--format json` voi nyt tuottaa kommentteja ilman
`historical_snippet`-kenttää siellä missä se ennen kaatui. Kenttä on jo
`omitempty` eikä koskaan ollut taattu. Levyvälimuisti ei myrkyty: `:614`
ohittaa kommentit joilla on snippet, joten seuraava ajo yrittää uudelleen.

## 1.2 Duplikaatti GraphQL-kutsu

**Juurisyy:** `(nil, nil)` on erottamaton tilasta "ei vielä haettu".
`gitremote/cache.go:27` palauttaa `([]string, error)`, joka ei voi ilmaista
"ei löytynyt", joten `service.go:653-657` tulkitsee tyhjän epäonnistumiseksi ja
kutsuu `fetchFileLines`ia uudelleen välimuistittomasti.

**Kutsusekvenssit per uniikki `(commit, path)`:**

| Tilanne | GraphQL-kutsuja nyt | Korjauksen jälkeen |
|---------|---------------------|--------------------|
| Löytyy paikallisesti | 0 | 0 |
| Puuttuu paikallisesti, löytyy etänä | 1 | 1 |
| Puuttuu kaikkialta | **2** | 1 |
| Etävirhe (rate limit) | **2** + kaatuminen | 1 + varoitus |
| M refreshiä, blob puuttuu | **2 + M** | 1 |

**Korjaus — kolmiarvoinen tulos:**

```go
// GetLines palauttaa found=false (ilman virhettä) kun blobia ei ole tässä
// commitissa. Negatiivinen tulos välimuistitetaan; virheitä ei.
func (c *Cache) GetLines(ctx context.Context, owner, repo, commit, path string) (lines []string, found bool, err error)
```

`(lines, found, err)` on parempi kuin `ErrNotFound`-sentinel: kutsupaikkoja on
tasan yksi, ja allekirjoitus pakottaa haarautumaan.

**Kriittinen sivuhyöty — testattavuus.** `NewService:36` tekee
`client.(*ghcli.Client)` -tyyppimuunnoksen, joten **fake-clientilla
`remoteCache` jää nil-arvoksi eikä yksikään testi aja tätä polkua**. Siksi bugi
pääsi läpi. Ota käyttöön kapea rajapinta ja rakenna cache aina:

```go
type FileLinesFetcher interface {
	FileLines(ctx context.Context, owner, repo, commit, path string) ([]string, error)
}
```

**Muutettavat kutsupaikat:**

| Paikka | Muutos |
|--------|--------|
| `gitremote/cache.go:10-42` | Kolmiarvoinen tulos, `FileLinesFetcher` |
| `threads/service.go:34-47` | Poista tyyppimuunnos, aina `gitremote.New(client)` |
| `threads/service.go` importit | Poista `ghcli` — rivi 36 on sen ainoa käyttö |
| `threads/service.go:645-659` | Haarauta `found`illa, älä yritä uudelleen |
| `threads/service.go:661-663` | **Poista** `fetchFileLines` (yhden kutsujan wrapper) |

**Testit:**
- `TestFetchLocalOrRemoteQueriesMissingFileOnce` — 5 kommenttia samalla
  `(commit, path)`, `FileLines` palauttaa `(nil, nil)` → **tasan 1 kutsu**.
  Pakko rakentaa `NewService`n kautta, muuten testi ei aja polkua.
- `TestFetchLocalOrRemoteReusesCacheAcrossFetches` — kaksi `attachHistorical-
  Snippets`-ajoa → 1 kutsu (nyt 3).
- Uusi `internal/gitremote/cache_test.go` (paketilla ei ole yhtään testiä):
  negatiivisen tuloksen välimuistitus, virheiden **ei**-välimuistitus,
  avainten erottelu, tyhjän commitin/polun hylkäys.

**Muuta siivottavaa samalla:** `fileKey` on määritelty kahdesti
(`gitremote/cache.go:15-18`, `threads/service.go:605-608`). Kun
`fetchLocalOrRemote` luottaa välimuistiin, `attachHistoricalSnippets`in oma
memo (`:610`) käy tarpeettomaksi — jäljelle jää yksi memo.

**Riski:** `Cache` ei ole säieturvallinen. Nyt yhdentekevää (yksi silmukka),
mutta ehdoton rakentaminen `NewService`ssa tekee siitä tavoitettavan myös
interaktiivisesta refresh-goroutinesta → lisää `sync.Mutex` tai kommentti.

---

# Vaihe 2 — render.go ✅ VALMIS (`077413b`)

## 2.1 `compactSnippetLines` (löydös 8) — tee tämä ensin

**Juurisyy:** `render.go:715-724` poistaa jokaisen tyhjän rivin, ei vain
reunapehmustetta. Ainoa kutsuja `printCommentBlock:592`.

**Ei kutsujaa, joka riippuisi sisätyhjien poistosta:** laatikon korkeus on
implisiittinen (`len(lines)+2`), mikään ei mittaa sitä jälkikäteen, ja
`wrapCommentSnippetLines:648-651` **säilyttää** tyhjät rivit tarkoituksella —
:715 kumoaa sen.

**Piilovika:** `TrimSpace` näkee ANSI-koodit ei-tyhjinä, joten värillisessä
tilassa sisätyhjät säilyvät ja putkitetussa eivät. **Tuloste eroaa TTY:n ja
putken välillä.** Korjaus yhtenäistää molemmat. Käytä olemassa olevaa
`ansiEscapeRegexp`iä (`render.go:702`):

```go
func isVisuallyBlank(line string) bool {
	return strings.TrimSpace(ansiEscapeRegexp.ReplaceAllString(line, "")) == ""
}
// compactSnippetLines karsii tyhjän pehmusteen vain reunoilta ja säilyttää
// sisäiset tyhjät rivit (kappalejaot, tyhjät rivit code fenceissä).
```

Palauta `nil` kokonaan tyhjälle syötteelle — se säilyttää nykyisen sopimuksen,
johon 2.2 nojaa.

**Testit:** reunojen karsinta, tyhjä rivi code fencen sisällä säilyy,
kappalejako säilyy, pelkkä ANSI-pehmuste → `nil`, päästä päähän
`TestPrintSummary_BoxKeepsParagraphBreaks`.

**Riski:** laatikot kasvavat rivin per kappalejako. Värillinen tuloste menettää
nykyisen turhan `\x1b[0m`-rivin (parannus, mutta näkyy golden-diffeissä).

## 2.2 Kommentin body voi kadota (löydös 5)

**Verdikti: osittain vahvistettu.** Kaksi laukaisupolkua:

- **(a)** `HighlightLine` snippetin `Lines`-alueen ulkopuolella —
  **ei tavoitettavissa tänään.** `snippetAround:685-711` rajaa `targetLine`n
  aina alueen sisään, ja `cacheVersion` on ollut 1 ensimmäisestä commitista, eli
  vanhoja välimuistitiedostoja ei ole. Puolustuksellinen.
- **(b)** body renderöityy nollaksi ei-tyhjäksi riviksi — **tavoitettavissa.**
  `renderCommentBody` käyttää `WithAutoStyle()`, joka putkitetulle tulosteelle
  on `notty`; runko `<!-- bot marker -->`, `<img …>` tai `&nbsp;` tuottaa pelkkää
  tyhjää → `printCommentBlock:593` palaa tulostamatta mitään.

Molemmat epäonnistuvat **täysin hiljaa**. TUI-renderöijä ei ole altis: se
tulostaa bodyn ja snippetin erikseen.

**Korjaus — tee "body tulostettiin" faktaksi, ei oletukseksi.**
`printHistoricalSnippet`in nykyinen `bool` tarkoittaa "snippet tulostettiin" ja
kutsuja **jättää sen huomiotta** (`render.go:95`). Määrittele se uudelleen ja
puskuroi snippet, jotta tulostusjärjestys säilyy (otsikko → body → snippet):

```go
// printCommentBlock kertoo, tulostiko se mitään.
func printCommentBlock(...) bool

// printHistoricalSnippet kertoo, tulostettiinko comment.Body osana snippettiä.
// Kutsujan on tulostettava body itse kun tämä palauttaa false.
func printHistoricalSnippet(...) (bodyEmitted bool)
```

Tämä **poistaa `hasSnippet`/`shouldRenderBody`-heuristiikan** (`render.go:85-86`)
kokonaan: päätöksen tekee se koodi joka oikeasti tulostaa.

**Testit:** highlight alueen ulkopuolella, `HighlightLine == 0`, tyhjäksi
renderöityvä body, `printCommentBlock`in paluuarvo suoraan, sekä olemassa
olevan `TestPrintSummary_SkipsDuplicateCommentWhenSnippetShown`in on pysyttävä
muuttumattomana.

---

# Vaihe 3 — TUI

## 3.0 Step 0 -siivous

Löydöksen 3 korjaus poistaa `buildThreadListLines`in `window`-parametrin ja
`selectionLine`-paluuarvon. Poista ne omana committina ennen muuta työtä.

## 3.1 Listakorkeus (löydös 3)

**Mitattu käytös** (6 threadia, 3 polkua):

| korkeus | tulos |
|---------|-------|
| 1 | **vain viimeinen polkuotsikko, ei yhtään threadia** — sama joka valinnalla |
| ≥ 2 | oikein |

**Kaksi lisävikaa, joita katselmointi ei löytänyt:**
1. Jos `listOffset > selectedThread` (valinta vierähtänyt **yläreunan** yli —
   mahdollista, koska `applyFilters:108` rajaa `listOffset`in muttei kutsu
   `ensureSelectionVisible`), `selectionLine == -1` ja silmukka vierittää
   **poispäin**. Sama täysi pimennys millä tahansa korkeudella.
2. `windowSize` (= `listHeight`) käytetään **threadien lukumääränä**
   (`model.go:221-229`) vaikka paneelin budjetti on **rivejä**. Polkuotsikot
   syövät rivejä → `listOffset` on systemaattisesti liian optimistinen. Juuri
   tätä uudelleenyrityssilmukka paikkasi.

**Mitattu hinta** (400 threadia, leveys 120): realistinen **8,14 ms/frame**,
patologinen (`listOffset=0`, valinta lopussa) **767 ms/frame**.

**Korjaus:** laske aloituskohta aritmeettisesti, älä yritä uudelleen. Rivihinta
on aidosti monotoninen `start`in suhteen (1 rivi per merkintä + 1 jos avaa
polun), joten yksi taaksepäin kävely valinnasta on tarkka:

```go
// threadListStart valitsee ensimmäisen piirrettävän threadin niin, että
// valittu thread mahtuu height riviin, vierittämättä mallin offsetia taemmas.
func threadListStart(list []threads.ReviewThread, desired, selected, height int) int
```

`buildThreadListLines` saa säännön, joka tekee korkeudesta 1 toimivan:
polkuotsikko ansaitsee rivin vain jos sen esittelemä merkintä mahtuu myös.
Ensimmäisen otsikon saa pudottaa, keskellä olevan ei — muuten sen alla oleva
merkintä näyttäisi kuuluvan edelliseen polkuun.

**Älä käytä `windowDetailBlock`/`detailAnchor`-mekanismia tähän.** Se
renderöi koko lohkon ensin ja viipaloi vasta sitten. Detail-paneelissa se on
yhden threadin kommentit (rajattu); listassa se tarkoittaisi PR:n **jokaisen**
threadin glamour-renderöintiä joka näppäinpainalluksella — täsmälleen se kulu
josta tässä bugissa on kyse. Lista on indeksoitavissa ja sen rivihinta
laskettavissa renderöimättä. Kaksi mekanismia on tässä oikein; yhtenäistä
nimeäminen, älä toteutusta.

**Mitattu korjauksen jälkeen:** 8,14 → **2,07 ms**; 767 → **1,85 ms**.

**Testit:** `TestRenderThreadListShowsSelectionAtEveryHeight` (taulukko
korkeudet {1,2,3,5} × jokainen valinta), `TestRenderThreadListIgnoresStaleOffset-
PastSelection`, `TestThreadListStartFitsSelection` (puhdas yksikkötesti).

## 3.2 Kutistettu vs. laajennettu (löydös 4)

**Mitattu** (8 kommenttia): korkeudella 4→laajennettu 1 / kutistettu 3,
12→2/3, 18→3/3. `vmax(3, …)`-lattia oli selvästi tarkoitettu laajennetulle ja
päätyi väärään haaraan.

`detailExpanded` on **vain** tämän yhden asian ohjain: `ToggleDetail`
(`model.go:165-167`), oletus `true`, ainoa muu käyttö `view.go:359`.

**Korjaus:**

```go
// detailCommentBudget kertoo, montako kommenttia detail-paneeli saa piirtää.
// Laajennettu mahtuu korkeuden mukaan; kutistettu on tiivis kurkistus
// valittuun kommenttiin eikä koskaan näytä enempää kuin laajennettu.
func detailCommentBudget(maxHeight int, expanded bool, total int) int
```

**Pakolliset testimuutokset** (ks. mahdottomuustodistus yllä) — molemmat
säilyttävät nimensä ja pääväitteensä, vain bugin koodannut sivuväite poistuu:
- `view_test.go:34` — poista `|| !strings.Contains(out, "second")`
- `view_test.go:58` — poista `|| !strings.Contains(out, "first")`

**Lisälöydös — omassa commitissani `63c9174`.** Ikkunointi käyttää rajattua
`selected`-arvoa (`view.go:365`) mutta korostusmerkki ja `anchor` raakaa
`state.selectedComment`ia (`:380`, `:386`, `:397`). Jos `selectedComment` on
alueen ulkopuolella, `anchor.start` jää −1:ksi ja `windowDetailBlock` palauttaa
ikkunoimattoman sisällön — eli juuri korjaamani bugi palaa. **Käytä
`selected`iä kaikissa kolmessa vertailussa.**

## 3.3 Status-suodatin (löydös 6)

**Juurisyy:** `ui.go:320-326` `if chosen { … }` ilman else-haaraa, putoaa
riville `:330` `m.state.state = StateView`.

**Sisarhaarat:** `"author"` ja teksti soveltavat arvon ehdoitta; huono tekijä
suodattaa nollaan threadiin, minkä lista raportoi. Status on **ainoa haara
jossa on validointiportti ilman epäonnistumispolkua**.

**`errMessage` näkyy** `renderBottomBar`issa (`view.go:480-481`), jonka
`RenderView:89` piirtää **jokaisessa tilassa** myös `StateFilter`issä. Siksi
oikea valinta on jäädä kehotteeseen: käyttäjä näkee viestin ja korjaa
kirjoitusvirheen ilman että joutuu painamaan `s` uudelleen.

Huomioi tyhjä syöte: tyhjällä kyselyllä kaikki kolme ehdotusta listataan ja
käyttäjä voi `tab`ata niihin, joten **tyhjä + korostettu ehdotus on silti
sovellettava**. Vain tyhjä ilman korostusta peruu.

**Testit:** tuntematon status → jää `StateFilter`iin, suodatin ennallaan,
`errMessage` sisältää `unknown status`, **ja** `renderBottomBar`in tuloste
sisältää sen (todistaa näkyvyyden, ei vain kentän arvoa); tyhjä peruu; tyhjä +
korostettu ehdotus soveltaa.

**Sivuhuomio:** `f` (`CycleStatusFilter`, `ui.go:252`) ei tyhjennä
`errMessage`a kuten `r`/`s`/`/`/`a` tekevät.

## 3.4 Markdown-välimuisti (löydös 7a)

**Mitattu:** `renderCommentMarkdown` = **195 µs/kutsu** realistiselle rungolle.
`threadPreview:189-211` ajaa sen jokaiselle näkyvälle threadille joka framessa,
ja `buildDetailContent:395` jokaiselle näkyvälle kommentille.

**Avain = rungon merkkijono, ei kommentin ID.** Renderöijä on pakettitason
singleton `WithWordWrap(0)`-asetuksella → tuloste on **leveysriippumaton**
(`threadPreview` katkaisee vasta jälkikäteen `xansi.Truncate`lla), väriprofiili
on kiinnitetty. Tuloste on siis rungon puhdas funktio. ID-avain **tarjoilisi
vanhentunutta ANSI:a**, koska GitHub-kommentteja voi editoida ja
`refreshFinished` (`ui.go:149-158`) vaihtaa threadit kokonaan. Sisältöavain
mitätöityy itse.

**Sijainti: pakettitaso, ei `Model`.** `Model` kopioidaan arvona kaikkialla
(`RenderView(state Model, …)`), ja se rakennetaan paljaana struct-literaalina
~15 testissä → kenttä olisi nil-map ilman paikkaa alustaa se laiskasti
arvovastaanottimista. Pakettitason muuttujat `markdownRenderer`in viereen
(`view.go:56-58`) ovat johdonmukainen valinta.

**Mitätöintiä ei tarvita:** `UpdateThreads`, `ReplyToThread` (lisää uuden
kommentin) ja `SetThreadStatus` (kääntää boolin) tuottavat kaikki uusia avaimia
tai eivät muuta avainta. Rajoita kasvu katolla (esim. 1024, tyhjennys kerralla).

**Säikeisyys:** `View()` on yksisäikeinen (bubbletea kutsuu sitä vain
tapahtumasilmukasta), mutta mutex kannattaa silti — välimuisti on pakettitilaa,
joka on tavoitettavissa rinnakkaisista testeistä, ja 195 µs säästöä vastaan
lukko on ilmainen. **Palauta kopio**, älä välimuistin taustataulukkoa.

**Mitattu (vaiheen 3.1 päälle):** 2,07 → **0,32 ms**; yhdessä 3.1:n kanssa
8,1 → 0,32 ms (25×) realistinen, 767 → 0,32 ms (2400×) patologinen.

**Jatkotyö:** `highlightSnippet` (`view.go:~770`) ajaa **toisen** glamour-
renderöinnin per frame ensimmäisen kommentin koodisnippetille. Sama hoito,
avain = polku + aloitusrivi + rivit yhdistettynä.

**Testit:** välimuistin tuloste == välimuistittoman, toinen kutsu osuu
(`len(markdownCache) == 1`), paluuarvo on kopio, katto pitää,
`BenchmarkRenderThreadList` regressiovahtina.

---

# Vaihe 4 — GraphQL-skeema (löydös 2)

**Juurisyy:** `render.go:396-411` — `end` suosii `OriginalLine`ia (sama
koordinaatisto kuin snippetillä), `start` tulee `StartLine`stä (nykyisen diffin
koordinaatisto). `originalStartLine`ia ei haeta lainkaan.

**Snippetin koordinaatisto varmistettu:** `attachHistoricalSnippets:617-620`
valitsee `OriginalLine`n, jos nil niin `Line`n, ja hakee tiedoston
`comment.CommitSHA`sta (= `originalCommit.oid`, `:586`). Snippetin avaruus on
siis tasan `firstNonNil(OriginalLine, Line)` — `end` on oikein, **`start` on
ainoa väärässä avaruudessa oleva arvo**.

**Kaksi oiretta:**
- Tiedosto siirtynyt muutaman rivin: väärä `start` osuu silti 15 rivin ikkunaan
  → **väärät alkuperäisrivit merkitään poistetuiksi, hiljaa**.
- Aidosti vanhentunut thread: GitHub palauttaa `line`/`startLine` nullina →
  `start = end` → **6-rivinen suggestion renderöityy yhdellä rivillä**.

**Valitse vaihtoehto (a), hae `originalStartLine`.** Vaihtoehto (b)
(palauta nil kun `Line != OriginalLine`) on aidosti huonompi: se ei tee mitään
vanhentuneelle tapaukselle ja **poistaa toimivaa tulostetta** siirtyneessä
tapauksessa sen sijaan että korjaisi sen.

**Muutokset:**

| Paikka | Muutos |
|--------|--------|
| `service.go:87`, `:117` | Lisää `originalStartLine` `startLine`n viereen |
| `service.go:201` (`ghComment`) | `OriginalStartLine *int` |
| `types.go:30` (`ThreadComment`) | `OriginalStartLine *int` |
| `service.go:583` (`convertComment`) | Välitä kenttä läpi |
| `render.go:396` | Valitse **pari**, älä yksittäisiä kenttiä |

Sääntö: jos `OriginalLine != nil`, käytä paria `(OriginalStartLine,
OriginalLine)`; muuten paria `(StartLine, Line)`. Älä koskaan sekoita.

**Välimuistipäätös — vaatii valinnan.** `threads/cache.go` persistoi
`ThreadComment`in JSONina, `cacheVersion = 1` (`cache.go:12`), ja `:614` ohittaa
kommentit joilla on jo snippet. Vanhat välimuistitiedostot deserialisoituvat
`OriginalStartLine == nil`. Kaksi vaihtoehtoa, suositus **molemmat**:
- Nosta `cacheVersion` → 2, jolloin vanhat merkinnät hylätään (yksi täysi
  uudelleenhaku per PR).
- Lisää `spanOf`-fallback, joka palauttaa ankkurin **pituuden**
  `(StartLine, Line)` -parista: pituus on kommentin ominaisuus, ei commitin.

**Erillinen sama vika:** `view.go:169` (`formatThreadLines`) vertaa
`thread.StartLine`ä (nykyinen avaruus) `thread.OriginalLine`en (alkuperäinen) ja
tulostaa esim. `"115-120"`. Korjaus vaatii `originalStartLine`in myös
thread-tasolle (`service.go:75`, `ghThread:215`, `types.go:42`,
`filter.go:58`). Vain näyttövirhe — kannattaa tehdä samassa muutoksessa.

**Testit:** `commentLineRange` yksikkötesteinä (suosii Original-paria; null
current-rivit; fallback current-pariin; yksirivinen; `spanOf`-fallback),
päästä päähän `TestRenderCommentSnippet_MultiLineSuggestionUsesOriginalRange`,
sekä `TestFetchReviewThreads_RequestsOriginalStartLine`.

**Riski:** matala. Yksi kutsuja, yksi ominaisuus (suggestion-diffit). Pahin
regressio on paluu raakaan ```suggestion-lohkoon, mikä on jo nykyinen käytös
aina kun alue karkaa ikkunasta.

---

# Verifiointi

```sh
gofmt -l ./internal ./cmd
go build ./... && go vet ./... && go test ./...
```

Projektissa ei ole linteriä eikä tyyppitarkistinta Go-työkaluketjun lisäksi.
`make test` ajaa `go test ./...`; **`go vet ./...` kannattaa lisätä
Makefileen**.

# Avoimet päätökset

0. ~~**Vaihe 1:** varoitusten ohjaus interaktiivisessa tilassa~~ — ratkaistu
   `io.Discard`illa (`Service.SetLogWriter`). Statusrivi jää parannukseksi.
1. **Vaihe 4:** nostetaanko `cacheVersion` 2:een (pakottaa uudelleenhaun) vai
   luotetaanko pelkkään `spanOf`-fallbackiin? Suositus: molemmat.
2. Tehdäänkö `formatThreadLines`in thread-tason korjaus samassa vaiheessa 4 vai
   erikseen?
