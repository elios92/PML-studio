# PML Studio --- Master Development Plan

**Documento di controllo sviluppo**\
**Scopo:** impedire sviluppo casuale, regressioni e rifacimenti
continui. Questo file è la fonte operativa da consultare prima di ogni
modifica a PML Studio.

------------------------------------------------------------------------

## 1. Visione del prodotto

PML Studio deve diventare un ambiente completo per creare fangame
Pokémon nativi per PC.

Pokémon Essentials/RPG Maker XP è una **sorgente di importazione e
compatibilità**, non l'ambiente finale di sviluppo. Dopo la conversione,
il progetto deve poter essere sviluppato, testato, esteso e compilato
interamente con PML Studio.

PML Studio deve coprire:

-   importazione e conversione di progetti Pokémon Essentials;
-   editor mappe;
-   tileset, autotile, collisioni, terrain tag e bordi;
-   regioni e connessioni fra mappe;
-   editor eventi no-code;
-   editor avanzato Python;
-   database completo del gioco;
-   Pokémon, mosse, abilità, strumenti, tipi, allenatori e incontri;
-   capitoli, missioni, quest e sistemi custom;
-   plugin;
-   runtime Python nativo;
-   Playtest/Debug;
-   compilazione Release;
-   packaging/installer Windows;
-   in futuro, sistemi online e multiplayer.

------------------------------------------------------------------------

## 2. Regola fondamentale

**Non si implementa una nuova funzione sopra una base instabile.**

Ogni funzione attraversa questo ciclo:

`SPECIFICA → IMPLEMENTAZIONE → COLLEGAMENTO AI DATI REALI → TEST → FIX → REGRESSION TEST → CONGELAMENTO`

Una funzione non è "finita" perché la UI esiste o perché il pulsante si
apre.

È finita solo quando:

1.  usa dati reali del progetto;
2.  salva correttamente;
3.  riapre correttamente;
4.  modifica realmente il comportamento del runtime quando previsto;
5.  gestisce gli errori senza crash;
6.  supera i test previsti;
7.  non rompe funzioni già approvate.

------------------------------------------------------------------------

## 3. Fonte di verità

### 3.1 Sorgente ufficiale

Deve esistere **una sola cartella sorgente ufficiale**.

Le build vecchie, TEST, backup e prototipi non devono essere usati come
base per nuove modifiche.

### 3.2 File congelati

Quando un sistema viene verificato e approvato, viene marcato:

`FROZEN`

Un modulo FROZEN non deve essere riscritto o rifattorizzato durante
lavori non correlati.

Può essere modificato solo se:

-   esiste un bug riproducibile;
-   una nuova funzione richiede realmente un'estensione;
-   viene documentato il motivo della modifica;
-   vengono rieseguiti i regression test del modulo.

### 3.3 Una funzione = una responsabilità

Evitare file monolitici e logica duplicata.

Separare progressivamente:

-   UI;
-   modello dati;
-   serializzazione;
-   conversione;
-   runtime;
-   validazione;
-   servizi;
-   test.

------------------------------------------------------------------------

## 4. Modalità PML Studio

### Standard --- No-Code

È la modalità principale.

L'utente configura il gioco tramite:

-   campi;
-   pulsanti;
-   menu;
-   finestre;
-   liste;
-   editor visuali;
-   eventi;
-   proprietà.

Quando una funzione non esiste originariamente in Essentials, PML Studio
può generare automaticamente il codice/runtime Python necessario.

**Regola:** una voce mostrata nello Standard deve essere realmente
funzionante. Vietate opzioni decorative o scollegate dal runtime.

### Advanced --- Developer

Espone:

-   proprietà interne;
-   identificatori;
-   dati grezzi;
-   diagnostica;
-   script Python;
-   API/plugin;
-   strumenti tecnici.

Standard e Advanced lavorano sullo **stesso formato progetto**.

------------------------------------------------------------------------

# 5. ROADMAP PRINCIPALE

## FASE 0 --- Stabilizzazione della base

**Priorità: BLOCCANTE**

Obiettivo: ottenere una base affidabile prima di aggiungere grandi
sistemi.

### Attività

-   [ ] Identificare definitivamente il sorgente ufficiale.
-   [ ] Verificare compilazione pulita con `go build .`.
-   [ ] EXE sempre denominato `PML Studio.exe`.
-   [ ] Eliminare freeze noti della UI.
-   [ ] Verificare apertura/chiusura progetto.
-   [ ] Verificare caricamento mappe.
-   [ ] Verificare cambio mappa ripetuto.
-   [ ] Verificare salvataggio senza perdita dati.
-   [ ] Verificare log diagnostico.
-   [ ] Creare regression checklist.
-   [ ] Separare chiaramente errori editor / convertitore / runtime.

### Gate di uscita

La fase è chiusa solo quando PML Studio può essere aperto, può caricare
un progetto, cambiare mappe, modificare e salvare senza freeze o
corruzione nei test base.

------------------------------------------------------------------------

## FASE 1 --- Core Editor Mappe

**Priorità: CRITICA**

### Funzioni

-   [ ] albero mappe;
-   [ ] rendering mappa;
-   [ ] layer;
-   [ ] selezione tiles;
-   [ ] disegno;
-   [ ] autotile;
-   [ ] blocco bordi;
-   [ ] zoom stabile;
-   [ ] salvataggio;
-   [ ] riapertura;
-   [ ] ridimensionamento pannelli;
-   [ ] import tileset PNG;
-   [ ] selezione tileset;
-   [ ] persistenza configurazione.

### Test obbligatorio

`Apri → modifica → salva → chiudi → riapri → verifica → Playtest`

Nessuna modifica grafica finale della toolbar ha priorità sui problemi
funzionali.

------------------------------------------------------------------------

## FASE 2 --- Collisioni, Movimenti e Terrain Tags

### Funzioni

-   [ ] Vista Movimenti Permessi.
-   [ ] Vista Terrain Tags separata.
-   [ ] Overlay leggibile sulla mappa.
-   [ ] Descrizione tag nel pannello laterale.
-   [ ] Collegamento ai dati reali.
-   [ ] Passabilità direzionale.
-   [ ] Cunette/ledge: blocco da un lato e salto dall'altro.
-   [ ] Compatibilità runtime.

### Terrain Tag custom riservati

18 ROCKCLIMB\
19 ROCKCLIMB\
20 WHIRLPOOL\
21 RUMPABREAKING\
22 RAPIDGROWTH\
23 ULTRATIMERISK\
24 BREZZOAREA\
25 THERMOMOTION\
26 TECORRUPTION\
27 WINDPATH\
28 DEEPWATER\
29 HEAVYFALL\
30 LAVASLIDE\
31 DARKWALK\
32 ICESLIDE\
33 SANDSTORM\
34 ACIDMIST\
35 SKYBRIDGE

### Gate

Un tile configurato in PML Studio deve produrre lo stesso comportamento
nel runtime senza configurazioni duplicate.

------------------------------------------------------------------------

## FASE 3 --- Regioni e Connessioni Mappe

### Regioni fisiche

Percorso previsto:

`converted/maps/regions/ATHERIA`\
`converted/maps/regions/NOVEPELAGO`\
`converted/maps/regions/ZEPHERIA`

### Funzioni

-   [ ] aggiungi regione;
-   [ ] rinomina;
-   [ ] sposta mappe;
-   [ ] aggiornamento fisico delle cartelle;
-   [ ] aggiornamento immediato albero;
-   [ ] protezione da cancellazioni accidentali;
-   [ ] connessioni N/S/E/O;
-   [ ] fino a due connessioni per direzione;
-   [ ] anteprima;
-   [ ] validazione;
-   [ ] salvataggio;
-   [ ] lettura runtime;
-   [ ] formato unico `plm.map_connections`.

### Gate

Editor e runtime devono interpretare le stesse connessioni senza
traduzioni parallele o database duplicati.

------------------------------------------------------------------------

## FASE 4 --- Header e Metadati Mappa

-   [ ] nome mappa;
-   [ ] regione;
-   [ ] capitolo;
-   [ ] missione;
-   [ ] proprietà ambientali;
-   [ ] musica;
-   [ ] incontri;
-   [ ] flag;
-   [ ] metadati;
-   [ ] spostamento regione;
-   [ ] persistenza completa.

------------------------------------------------------------------------

## FASE 5 --- Editor Eventi

**Sistema fondamentale. Non implementare tutto in una volta.**

### 5A --- Eventi base

-   [ ] crea;
-   [ ] modifica;
-   [ ] elimina;
-   [ ] copia;
-   [ ] duplica;
-   [ ] sposta;
-   [ ] rinomina;
-   [ ] pagine evento;
-   [ ] grafica;
-   [ ] condizioni;
-   [ ] trigger.

### 5B --- Switch

Default: **disabilitato**, nessun numero preimpostato.

Quando viene attivato:

-   apre selettore;
-   mostra switch esistenti;
-   permette creazione/nome;
-   permette condizione ON/OFF;
-   mostra chiaramente ON/OFF nella finestra evento.

### 5C --- Variabili

-   selettore dedicato;
-   creazione/nome;
-   operatori semplificati;
-   valori;
-   condizioni;
-   anteprima del risultato/comportamento nella finestra evento.

### 5D --- Comandi visuali

-   [ ] testo;
-   [ ] movimento;
-   [ ] Warp;
-   [ ] switch;
-   [ ] variabile;
-   [ ] oggetto;
-   [ ] lotta;
-   [ ] allenatore;
-   [ ] audio;
-   [ ] animazione;
-   [ ] condizioni;
-   [ ] scelta;
-   [ ] attesa;
-   [ ] trasferimento;
-   [ ] altri comandi equivalenti utili di RPG Maker.

### 5E --- Sceneggiatura → Python

Per logiche più complesse:

l'utente descrive il comportamento in forma strutturata/semplificata e
PML Studio lo converte in logica Python compatibile con il runtime.

Il codice generato deve essere validato prima del
salvataggio/esecuzione.

### 5F --- Script avanzato

Editor Python dedicato per utenti Advanced.

------------------------------------------------------------------------

## FASE 6 --- Vista Dati / Database

Creare prima il framework comune, poi aggiungere le categorie.

### Categorie

-   [ ] Pokémon;
-   [ ] forme;
-   [ ] evoluzioni;
-   [ ] mosse;
-   [ ] abilità;
-   [ ] strumenti;
-   [ ] tipi;
-   [ ] allenatori;
-   [ ] squadre;
-   [ ] incontri selvatici;
-   [ ] mappe;
-   [ ] metadati;
-   [ ] switch globali;
-   [ ] variabili globali;
-   [ ] impostazioni lotta;
-   [ ] impostazioni generali.

### Regola Standard/Advanced

Standard mostra dati comprensibili e controlli no-code.

Advanced può mostrare ID, proprietà interne, strutture e configurazioni
tecniche.

------------------------------------------------------------------------

## FASE 7 --- Sistema Lotta e Meccaniche Pokémon

Non limitarsi alle capacità originali di Essentials.

Configurazioni previste:

-   [ ] dimensione squadra configurabile, incluso supporto fino a 10
    quando abilitato;
-   [ ] lotte singole;
-   [ ] doppie;
-   [ ] triple quando supportate;
-   [ ] incontri selvatici multipli fino a 5 quando abilitati;
-   [ ] Mega Evoluzioni;
-   [ ] Risvegli/meccaniche custom;
-   [ ] level cap;
-   [ ] tipi custom;
-   [ ] regole di battaglia;
-   [ ] AI;
-   [ ] cattura;
-   [ ] esperienza;
-   [ ] evoluzione;
-   [ ] status;
-   [ ] mosse;
-   [ ] abilità.

Quando PML Studio espone una meccanica nuova, deve generare/abilitare
automaticamente il supporto runtime necessario.

------------------------------------------------------------------------

## FASE 8 --- Convertitore Essentials → PML

Supporto prioritario:

-   Essentials v20.1;
-   Essentials v21.1.

### Pipeline

`ANALISI → IMPORT ASSET → DATI → MAPPE → EVENTI → SCRIPT SUPPORTATI → PLUGIN → VALIDAZIONE → REPORT`

Ogni conversione deve classificare gli elementi:

-   **Convertito**
-   **Convertito con adattamento**
-   **Parziale**
-   **Non supportato**
-   **Errore**

Mai fingere che una conversione sia riuscita quando una parte non è
stata interpretata.

------------------------------------------------------------------------

## FASE 9 --- Plugin System

### Plugin Manager PLM

-   [ ] installa;
-   [ ] disinstalla;
-   [ ] abilita;
-   [ ] disabilita;
-   [ ] configurazione;
-   [ ] dipendenze;
-   [ ] compatibilità;
-   [ ] diagnostica.

### Plugin Converter

Analizza plugin Ruby Essentials:

-   hook;
-   classi;
-   dipendenze;
-   asset;
-   configurazioni;
-   comportamento.

Produce equivalente PLM/Python solo quando la conversione è
verificabile.

### Compatibility Knowledge Base

Archivio incrementale di:

-   pattern Ruby;
-   equivalenti Python;
-   API Essentials;
-   API PLM;
-   regole di migrazione;
-   incompatibilità conosciute;
-   test associati.

------------------------------------------------------------------------

## FASE 10 --- Playtest / Debug

Il pulsante **Play** deve avviare il progetto in modalità sviluppatore.

Funzioni previste:

-   menu Debug;
-   switch;
-   variabili;
-   squadra;
-   Pokémon;
-   strumenti;
-   denaro;
-   medaglie;
-   teletrasporto;
-   mappe;
-   incontri;
-   test battaglia;
-   diagnostica;
-   log.

La Release finale non deve esporre strumenti sviluppatore non previsti.

------------------------------------------------------------------------

## FASE 11 --- Build e Distribuzione

### Debug

`PML Studio → Play → runtime Debug`

### Release

`PML Studio → Build → validazione → runtime Release → pacchetto gioco`

### Obiettivi

-   [ ] EXE nativo Windows;
-   [ ] dipendenze incluse;
-   [ ] asset corretti;
-   [ ] configurazione;
-   [ ] salvataggi;
-   [ ] icona;
-   [ ] versione;
-   [ ] installer;
-   [ ] verifica su installazione pulita.

------------------------------------------------------------------------

## FASE 12 --- Multiplayer / Online

Da affrontare **dopo la stabilità del gioco offline**.

-   [ ] architettura networking;
-   [ ] server;
-   [ ] stanze private;
-   [ ] codice stanza;
-   [ ] sincronizzazione giocatori;
-   [ ] Sala Connessione;
-   [ ] lotte PC ↔ PC;
-   [ ] scambi;
-   [ ] sicurezza;
-   [ ] versionamento protocollo.

Il multiplayer non deve contaminare il core offline.

------------------------------------------------------------------------

# 6. ORDINE DI LAVORO OBBLIGATORIO

Quando iniziamo una sessione di sviluppo:

### PASSO 1 --- Stato

Indicare:

-   fase corrente;
-   funzione corrente;
-   file coinvolti;
-   cosa è già FROZEN;
-   bug conosciuti.

### PASSO 2 --- Obiettivo singolo

Definire un risultato verificabile.

Esempio corretto:

> "La selezione di uno switch nell'editor evento deve essere salvata e
> ricaricata correttamente."

Esempio sbagliato:

> "Miglioriamo gli eventi."

### PASSO 3 --- Analisi prima del codice

Prima di modificare:

1.  trovare il codice esistente;
2.  capire il flusso dati;
3.  identificare chi legge e chi scrive quei dati;
4.  controllare dipendenze;
5.  decidere la modifica minima.

### PASSO 4 --- Implementazione

Modificare solo i file necessari.

### PASSO 5 --- Compilazione

La compilazione deve riuscire senza errori.

### PASSO 6 --- Test mirato

Testare la funzione appena implementata.

### PASSO 7 --- Regression test

Controllare le funzioni vicine già approvate.

### PASSO 8 --- Stato

Aggiornare questo documento o il registro di sviluppo.

------------------------------------------------------------------------

# 7. REGOLA ANTI-"TENTATIVI"

Non correggere un bug modificando codice casualmente finché "sembra
funzionare".

Per ogni bug:

## BUG ID

Esempio: `MAP-014`

## Riproduzione

Passaggi esatti.

## Risultato atteso

Cosa deve succedere.

## Risultato reale

Cosa succede.

## Area responsabile

UI / dati / serializer / runtime / converter.

## Causa

Da compilare dopo l'analisi.

## Fix

File e funzione modificati.

## Test

Test che dimostra la correzione.

## Regressioni

Funzioni correlate ricontrollate.

Se la causa non è ancora nota, lo stato è `IN ANALISI`, non `FIXED`.

------------------------------------------------------------------------

# 8. DEFINITION OF DONE

Una funzione può essere marcata **DONE/FROZEN** solo se:

-   [ ] compila;
-   [ ] si apre;
-   [ ] funziona;
-   [ ] salva;
-   [ ] ricarica;
-   [ ] usa il formato dati ufficiale;
-   [ ] è collegata al runtime se necessario;
-   [ ] gestisce input errati;
-   [ ] non introduce freeze;
-   [ ] non introduce crash;
-   [ ] regression test superati;
-   [ ] nessun placeholder spacciato per funzione completa.

------------------------------------------------------------------------

# 9. STATI UFFICIALI

Usare esclusivamente:

`PLANNED` --- pianificato\
`IN ANALYSIS` --- analisi in corso\
`IN DEVELOPMENT` --- implementazione\
`BLOCKED` --- bloccato\
`TESTING` --- test\
`DONE` --- completato\
`FROZEN` --- verificato e da non toccare\
`REGRESSION` --- funzione precedentemente funzionante ora rotta

------------------------------------------------------------------------

# 10. REGISTRO DI SVILUPPO

Aggiornare questa tabella durante lo sviluppo.

  ------------------------------------------------------------------------------------
  ID             Sistema         Stato          Ultimo test    Note
  -------------- --------------- -------------- -------------- -----------------------
  CORE-001       Avvio editor    TESTING        ---            Verificare baseline

  MAP-001        Rendering mappe TESTING        ---            Verificare sorgente
                                                               corrente

  MAP-002        Disegno/layer   TESTING        ---            ---

  TERR-001       Movimenti       TESTING        ---            ---
                 permessi                                      

  TERR-002       Terrain Tags    TESTING        ---            Collegamento runtime
                                                               obbligatorio

  CONN-001       Connessioni     TESTING        ---            Formato
                 mappe                                         `plm.map_connections`

  REGION-001     Gestione        TESTING        ---            Controllare spostamento
                 regioni                                       fisico

  EVENT-001      Editor eventi   IN DEVELOPMENT ---            Sviluppare per blocchi

  DATA-001       Vista Dati      PLANNED        ---            Framework comune prima

  CONV-001       Converter       PLANNED        ---            Pipeline da consolidare
                 Essentials                                    

  PLUGIN-001     Plugin Manager  PLANNED        ---            ---

  DEBUG-001      Playtest Debug  PLANNED        ---            ---

  BUILD-001      Build Release   PLANNED        ---            ---

  NET-001        Multiplayer     PLANNED        ---            Solo dopo offline
                                                               stabile
  ------------------------------------------------------------------------------------

Gli stati iniziali qui sopra sono **provvisori**: devono essere
verificati sul sorgente ufficiale prima di promuovere un modulo a
DONE/FROZEN.

------------------------------------------------------------------------

# 11. REGRESSION CHECKLIST MINIMA

Prima di consegnare una build significativa:

-   [ ] PML Studio si avvia.
-   [ ] Un progetto si apre.
-   [ ] L'albero mappe appare.
-   [ ] Una mappa viene caricata.
-   [ ] È possibile cambiare mappa più volte.
-   [ ] Il canvas resta responsivo.
-   [ ] Disegno e layer funzionano.
-   [ ] Salva funziona.
-   [ ] Riaprendo il progetto la modifica esiste ancora.
-   [ ] Movimenti permessi funzionano.
-   [ ] Terrain Tags funzionano.
-   [ ] Connessioni vengono mantenute.
-   [ ] Regioni vengono mantenute.
-   [ ] Eventi esistenti non vengono corrotti.
-   [ ] Playtest parte.
-   [ ] Il runtime legge i dati modificati.
-   [ ] Nessun crash.
-   [ ] Nessun freeze evidente.
-   [ ] Il log non contiene nuovi errori critici.

------------------------------------------------------------------------

# 12. POLITICA DELLE MODIFICHE

Per fix e aggiornamenti:

-   consegnare solo i file realmente modificati;
-   se viene creato un nuovo file/cartella, indicare esattamente dove
    copiarlo;
-   non incrementare la versione per ogni piccolo fix;
-   quando viene prodotta una build di test significativa, ricompilare
    dalla stessa sorgente;
-   l'eseguibile deve chiamarsi sempre `PML Studio.exe`;
-   non sostituire sistemi già verificati con nuove implementazioni
    senza motivo tecnico.

------------------------------------------------------------------------

# 13. ARCHITETTURA DATI --- PRINCIPIO

PML Studio e runtime devono condividere una definizione coerente dei
dati.

Evitare:

`Editor → formato A → conversione → formato B → runtime`

quando non è necessario.

Preferire:

`PML Studio ↔ formato progetto PLM ↔ runtime`

Il convertitore Essentials deve essere un ingresso:

`Essentials → Converter → formato PLM`

non una dipendenza permanente del runtime.

------------------------------------------------------------------------

# 14. PRIORITÀ ATTUALE

Ordine consigliato:

1.  stabilità;
2.  mappe;
3.  collisioni/Terrain Tags;
4.  connessioni;
5.  regioni;
6.  header/metadati;
7.  eventi;
8.  database;
9.  collegamento completo runtime;
10. converter;
11. plugin;
12. debug completo;
13. build/release;
14. multiplayer.

La grafica finale dell'interfaccia viene rifinita **dopo** la stabilità
funzionale.

------------------------------------------------------------------------

# 15. PROTOCOLLO PER CHATGPT / CODEX / ALTRI AGENTI

Prima di scrivere codice, l'agente deve leggere questo file.

L'agente deve:

1.  identificare la fase della roadmap;
2.  analizzare il sorgente corrente, non affidarsi a versioni ricordate;
3.  non inventare file, API o strutture non presenti senza dichiararlo;
4.  non modificare moduli FROZEN senza motivo documentato;
5.  scegliere la modifica minima compatibile con l'architettura;
6.  mantenere editor e runtime sincronizzati;
7.  compilare dopo le modifiche;
8.  eseguire test mirati;
9.  indicare esattamente i file modificati;
10. indicare cosa è stato verificato realmente e cosa resta da
    verificare;
11. non dichiarare "completato" ciò che è solo UI, mock o placeholder;
12. fermarsi e analizzare la causa quando un fix fallisce, invece di
    accumulare patch casuali.

------------------------------------------------------------------------

# 16. FORMATO RAPPORTO DI FINE STEP

Ogni step deve terminare con:

**STEP:**\
**Obiettivo:**\
**Stato:** DONE / TESTING / BLOCKED\
**File modificati:**\
**Funzioni implementate:**\
**Test eseguiti:**\
**Regression test:**\
**Problemi rimasti:**\
**Prossimo step:**

Questo permette di riprendere il progetto in una nuova conversazione
senza perdere la linea di sviluppo.

------------------------------------------------------------------------

## Regola finale

**PML Studio deve crescere per sistemi completi e verificati, non per
quantità di pulsanti o codice scritto.**

Quando una fase non supera il proprio gate, si resta su quella fase. Le
nuove idee vengono registrate nella roadmap, ma non interrompono
automaticamente il lavoro corrente.


---

# 17. PROTOCOLLO AUDIT 100% DEL SORGENTE

Questo protocollo deve essere eseguito periodicamente e obbligatoriamente prima di dichiarare PML Studio stabile o prima di una release importante.

L'audit non consiste nel verificare soltanto che il programma compili. Deve cercare attivamente bug, regressioni, dati scollegati, funzioni incomplete, errori di UX e discrepanze tra editor e runtime.

## 17.1 Livelli di controllo

### A — Struttura sorgente
- [ ] Inventario completo di file e cartelle.
- [ ] Individuazione di file duplicati, obsoleti o apparentemente inutilizzati.
- [ ] Individuazione di codice duplicato.
- [ ] Ricerca di TODO/FIXME/HACK/placeholder.
- [ ] Ricerca di funzioni vuote o stub.
- [ ] Ricerca di errori ignorati.
- [ ] Ricerca di panic/crash potenziali.
- [ ] Controllo dipendenze e import.
- [ ] Controllo path assoluti o dipendenti dal PC dello sviluppatore.
- [ ] Controllo gestione Unicode e nomi file.
- [ ] Controllo concorrenza/goroutine e possibili deadlock della UI.

### B — Compilazione
- [ ] `go build .`
- [ ] eventuali test automatici.
- [ ] analisi statica disponibile.
- [ ] verifica warning/errori.
- [ ] build Windows x64.
- [ ] avvio dell'EXE appena compilato.

### C — Persistenza dati
Per ogni editor:
`CARICA → MODIFICA → SALVA → CHIUDI → RIAPRI → CONFRONTA`

Controllare:
- [ ] perdita dati;
- [ ] valori default inseriti involontariamente;
- [ ] ID modificati;
- [ ] ordine elementi;
- [ ] riferimenti fra entità;
- [ ] encoding;
- [ ] file corrotti;
- [ ] salvataggi parziali.

### D — UI
Controllare sistematicamente:
- [ ] testi tagliati;
- [ ] controlli sovrapposti;
- [ ] finestre troppo piccole;
- [ ] ridimensionamento;
- [ ] DPI/scaling Windows;
- [ ] focus tastiera;
- [ ] tab order;
- [ ] doppio click;
- [ ] menu;
- [ ] scrollbar;
- [ ] selezioni;
- [ ] stato dei pulsanti;
- [ ] refresh dopo modifica;
- [ ] finestre modali;
- [ ] freeze;
- [ ] operazioni ripetute molte volte.

### E — Editor ↔ Runtime
Ogni proprietà che influenza il gioco deve essere verificata nel runtime reale.

`PML STUDIO → SALVATAGGIO → DATI PLM → RUNTIME → COMPORTAMENTO`

Se editor e runtime hanno due interpretazioni diverse, il problema è BLOCCANTE.

### F — Conversione Essentials
Usare progetti campione e verificare:
- [ ] mappe;
- [ ] tileset;
- [ ] autotile;
- [ ] collisioni;
- [ ] terrain tag;
- [ ] eventi;
- [ ] switch;
- [ ] variabili;
- [ ] warp;
- [ ] allenatori;
- [ ] incontri;
- [ ] Pokémon;
- [ ] mosse;
- [ ] strumenti;
- [ ] tipi;
- [ ] metadati;
- [ ] audio;
- [ ] grafica;
- [ ] plugin supportati.

### G — Stress test
Ripetere operazioni per trovare problemi intermittenti:
- [ ] cambiare mappa almeno 50 volte;
- [ ] aprire/chiudere finestre ripetutamente;
- [ ] salvare più volte;
- [ ] cambiare vista rapidamente;
- [ ] modificare regioni e ricaricare;
- [ ] creare/eliminare eventi;
- [ ] usare Undo/Redo quando disponibile;
- [ ] avviare/chiudere Playtest più volte;
- [ ] aprire progetti grandi;
- [ ] testare progetto con centinaia di mappe.

---

# 18. BUG E DIFETTI GIÀ SEGNALATI — DA RICONTROLLARE

Questa sezione è un registro permanente. Un bug non viene cancellato: quando risolto passa a `VERIFIED/FROZEN` con indicazione del test effettuato.

| ID | Area | Problema segnalato | Stato iniziale | Verifica richiesta |
|---|---|---|---|---|
| BUG-UI-001 | UI generale | Testi tagliati in alcune finestre/controlli | DA VERIFICARE | DPI 100/125/150%, resize finestra |
| BUG-CONN-001 | Connessioni | Testi tagliati nella UI Connessioni | DA VERIFICARE | Aprire UI e provare resize/scaling |
| BUG-UI-002 | Layout | Pannelli laterali devono essere ridimensionabili | DA VERIFICARE | Resize continuo e persistenza |
| BUG-MAP-001 | Mappe | Freeze UI dopo selezione mappa | DA VERIFICARE | Cambio mappe ripetuto + log |
| BUG-MAP-002 | Mappe | Caricamento mappe lento | DA VERIFICARE | Progetto grande / misurazione tempi |
| BUG-MAP-003 | Mappe | Zoom può bloccare l'editor | DA VERIFICARE | Stress zoom + cambio mappa |
| BUG-REG-001 | Regioni | Mappa 609 risultata cancellata durante gestione regioni | CRITICO / DA VERIFICARE | Test copia/sposta + controllo filesystem |
| BUG-REG-002 | Regioni | Albero mappe non sempre ricaricato dopo modifica regione | DA VERIFICARE | Sposta mappa e verifica refresh immediato |
| BUG-FOLDER-001 | File dialog | Selettore cartelle vecchio/stile Win98 | DA RIVEDERE | Verificare dialog corrente |
| BUG-ICON-001 | Windows | Icona EXE/finestra non sempre corretta | DA VERIFICARE | Build pulita Windows |
| BUG-EVT-001 | Eventi | Switch presenta numeri/default quando dovrebbe partire disabilitato | DA IMPLEMENTARE/VERIFICARE | Nuovo evento senza condizioni |
| BUG-EVT-002 | Eventi | Selettore switch deve usare switch reali e permettere ON/OFF | DA IMPLEMENTARE | Salva/riapri + runtime |
| BUG-EVT-003 | Eventi | Variabili da semplificare e collegare ai dati reali | DA IMPLEMENTARE | Salva/riapri + runtime |
| BUG-EVT-004 | Eventi | Serve anteprima visiva del comportamento variabile | DA IMPLEMENTARE | Preview coerente con runtime |
| BUG-CONN-002 | Connessioni | Editor e runtime devono usare un solo formato connessioni | DA VERIFICARE | Round-trip `plm.map_connections` |
| BUG-TERR-001 | Terrain | Terrain Tags devono essere condivisi con runtime | DA VERIFICARE | Configura editor → test runtime |
| BUG-TERR-002 | Movimento | Cunette: non deve salire dal lato vietato e deve saltare dal lato corretto | DA VERIFICARE | Test tutte le direzioni |
| BUG-DATA-001 | Dati | Rischio opzioni UI presenti ma non realmente collegate al progetto/runtime | AUDIT GLOBALE | Tracciare ogni controllo fino al consumer |
| BUG-SAVE-001 | Persistenza | Verificare che tutte le modifiche sopravvivano a chiusura/riapertura | AUDIT GLOBALE | Round-trip di tutte le viste |

---

# 19. MATRICE DI TRACCIABILITÀ FUNZIONALE

Per ogni funzione importante creare una riga con questa catena:

| Funzione | UI | Modello dati | Serializer | File progetto | Runtime consumer | Test | Stato |
|---|---|---|---|---|---|---|---|

Una funzione con una casella mancante nella catena non può essere considerata DONE.

Esempio:

| Funzione | UI | Modello dati | Serializer | File progetto | Runtime consumer | Test | Stato |
|---|---|---|---|---|---|---|---|
| Terrain Tag | sì | sì | sì | progetto PLM | movimento runtime | round-trip + gioco | TESTING |

Questa matrice serve soprattutto a trovare i "pulsanti finti": controlli UI implementati ma senza effetto reale.

---

# 20. TEST DI NON DISTRUZIONE

PML Studio deve proteggere il progetto dell'utente.

Per ogni funzione che scrive sul disco:

- [ ] verificare esistenza sorgente;
- [ ] evitare cancellazioni implicite;
- [ ] scrivere in modo atomico quando possibile;
- [ ] non sovrascrivere dati sconosciuti senza necessità;
- [ ] validare prima del commit;
- [ ] gestire errore di scrittura;
- [ ] mantenere backup/rollback dove l'operazione è distruttiva;
- [ ] verificare che uno spostamento non diventi cancellazione;
- [ ] testare nomi duplicati;
- [ ] testare file mancanti;
- [ ] testare cartelle mancanti;
- [ ] testare permessi insufficienti.

Le operazioni potenzialmente distruttive devono essere considerate CRITICHE.

---

# 21. TEST MATRIX PER OGNI NUOVA FUNZIONE

Ogni funzione deve essere provata almeno in questi casi:

1. **Happy path** — uso normale.
2. **Empty state** — progetto/dato vuoto.
3. **Boundary** — valori minimi/massimi.
4. **Invalid input** — dati errati.
5. **Persistence** — chiudi e riapri.
6. **Repeated use** — ripeti molte volte.
7. **Cross-view** — cambia vista e torna.
8. **Runtime** — verifica nel gioco quando applicabile.
9. **Legacy import** — verifica dati convertiti da Essentials quando applicabile.
10. **Regression** — verifica le funzioni adiacenti già FROZEN.

---

# 22. SEVERITÀ BUG

`S0 — DATA LOSS`
Perdita/corruzione progetto. Blocca immediatamente lo sviluppo della funzione.

`S1 — CRASH/FREEZE`
Crash, deadlock, editor inutilizzabile.

`S2 — FUNZIONE ERRATA`
Il programma continua ma produce dati/comportamento sbagliato.

`S3 — UX`
Funzione utilizzabile ma scomoda, testi tagliati, refresh, layout.

`S4 — COSMETICO`
Aspetto grafico senza impatto funzionale.

Priorità obbligatoria:
`S0 → S1 → S2 → S3 → S4`

---

# 23. AUDIT REPORT

Al termine di ogni audit produrre:

**Versione/base analizzata:**  
**Commit/build/hash se disponibile:**  
**Compilazione:** PASS/FAIL  
**File analizzati:**  
**Test automatici:**  
**Test manuali:**  
**Bug S0:**  
**Bug S1:**  
**Bug S2:**  
**Bug S3:**  
**Bug S4:**  
**Regressioni:**  
**Funzioni scollegate dal runtime:**  
**Placeholder/stub trovati:**  
**Codice duplicato/rischioso:**  
**Moduli FROZEN verificati:**  
**Gate roadmap superati:**  
**Gate non superati:**  
**Prossimo intervento consigliato:**  

Nessun audit può dichiarare "100% verificato" se esistono aree non eseguite o non testabili. In quel caso devono essere indicate esplicitamente come `NON VERIFICATE`.

---

# 24. REGOLA DI RIPRESA DEL PROGETTO

All'inizio di una nuova conversazione o sessione:

1. leggere questo MASTER;
2. leggere l'ultimo AUDIT REPORT;
3. leggere l'ultimo STEP REPORT;
4. identificare il sorgente ufficiale;
5. controllare i bug aperti;
6. lavorare sul primo problema non risolto della fase corrente.

Non ricostruire lo stato del progetto soltanto dalla memoria della conversazione.
