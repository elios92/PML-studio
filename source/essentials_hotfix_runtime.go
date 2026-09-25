//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type essentialsHotfixCoverageEntry struct {
	ID     string `json:"id"`
	Area   string `json:"area"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

type essentialsHotfixCoverage struct {
	Schema        string                         `json:"schema"`
	Version       int                            `json:"version"`
	Profile       string                         `json:"profile"`
	PluginVersion string                         `json:"plugin_version"`
	Entries       []essentialsHotfixCoverageEntry `json:"entries"`
}

func loadExactEssentialsHotfixProfile(dest string) (essentialsHotfixProfile, bool) {
	var profile essentialsHotfixProfile
	data, err := os.ReadFile(filepath.Join(dest, "converted", "essentials_hotfixes.json"))
	if err != nil || json.Unmarshal(data, &profile) != nil {
		return profile, false
	}
	return profile, profile.Detected && profile.ExactOfficial107 &&
		profile.NativeProfile == essentialsV201HotfixProfile
}

func replaceRequired(data []byte, old, replacement, label string) ([]byte, error) {
	if !bytes.Contains(data, []byte(old)) {
		return nil, fmt.Errorf("v20.1 Hotfixes: pattern runtime non trovato: %s", label)
	}
	return bytes.Replace(data, []byte(old), []byte(replacement), 1), nil
}

func installEssentialsV201HotfixRuntime(dest string) error {
	profile, enabled := loadExactEssentialsHotfixProfile(dest)
	if !enabled {
		return nil
	}

	coverage := essentialsHotfixCoverage{
		Schema:        "pml.essentials_hotfix_coverage",
		Version:       1,
		Profile:       essentialsV201HotfixProfile,
		PluginVersion: profile.PluginVersion,
	}
	add := func(id, area, status, note string) {
		coverage.Entries = append(coverage.Entries, essentialsHotfixCoverageEntry{
			ID: id, Area: area, Status: status, Note: note,
		})
	}

	// ---------------------------------------------------------------------
	// Battle/capture hotfixes whose semantics differ from the clean v20.1
	// base and therefore require explicit Python equivalents.
	// ---------------------------------------------------------------------
	capturePath := filepath.Join(dest, "game", "capture_system.py")
	capture, err := os.ReadFile(capturePath)
	if err != nil {
		return err
	}
	capture, err = replaceRequired(capture,
		`def capture_value(target: dict[str, Any], species_data: dict[str, Any], multiplier: float) -> int:
    if math.isinf(multiplier):
        return 255
    maximum = max(1, int(target.get("stats", {}).get("hp", 1)))
    hp = max(1, int(target.get("hp", maximum)))
    rate = max(1, int(species_data.get("CatchRate", 45) or 45))`,
		`def capture_value(target: dict[str, Any], species_data: dict[str, Any], multiplier: float, ball: str | None = None) -> int:
    if math.isinf(multiplier):
        return 255
    maximum = max(1, int(target.get("stats", {}).get("hp", 1)))
    hp = max(1, int(target.get("hp", maximum)))
    rate = max(1, int(species_data.get("CatchRate", 45) or 45))
    if str(ball or "").upper() == "HEAVYBALL":
        try:
            weight_hg = round(float(species_data.get("Weight", 0) or 0) * 10)
        except (TypeError, ValueError):
            weight_hg = 0
        if weight_hg >= 3000:
            rate += 30
        elif weight_hg >= 2000:
            rate += 20
        elif weight_hg < 1000:
            rate -= 20
        rate = max(1, min(255, rate))`,
		"Heavy Ball catch rate")
	if err != nil {
		return err
	}
	capture, err = replaceRequired(capture,
		`def throw_ball(target: dict[str, Any], species_data: dict[str, Any], multiplier: float,
               rng: random.Random | Any = random) -> tuple[bool, int]:
    """Restituisce (catturato, oscillazioni completate)."""
    value = capture_value(target, species_data, multiplier)`,
		`def throw_ball(target: dict[str, Any], species_data: dict[str, Any], multiplier: float,
               rng: random.Random | Any = random, *, ball: str | None = None) -> tuple[bool, int]:
    """Restituisce (catturato, oscillazioni completate)."""
    value = capture_value(target, species_data, multiplier, ball)`,
		"Heavy Ball throw context")
	if err != nil {
		return err
	}
	if err := writeBytesAtomic(capturePath, capture, 0644); err != nil {
		return err
	}

	wildPath := filepath.Join(dest, "game", "wild_battle.py")
	wild, err := os.ReadFile(wildPath)
	if err != nil {
		return err
	}
	wild, err = replaceRequired(wild,
		`        caught, shakes = throw_ball(self.enemy, species_data, multiplier)`,
		`        caught, shakes = throw_ball(self.enemy, species_data, multiplier, ball=ball)`,
		"Heavy Ball wild battle")
	if err != nil {
		return err
	}
	if err := writeBytesAtomic(wildPath, wild, 0644); err != nil {
		return err
	}
	add("BATTLE-001", "capture", "adapted", "Heavy Ball applica l'addendo al CatchRate prima del moltiplicatore.")

	battlePath := filepath.Join(dest, "game", "battle_mechanics.py")
	battle, err := os.ReadFile(battlePath)
	if err != nil {
		return err
	}
	battle, err = replaceRequired(battle,
		`        # Queste mosse non devono creare catene ricorsive/auto-copie.
        return move_id not in {
            "COPYCAT", "MIMIC", "SKETCH", "MIRRORMOVE", "METRONOME",
            "ASSIST", "SLEEPTALK", "INSTRUCT", "MEFIRST",
        }`,
		`        # Queste mosse non devono creare catene ricorsive/auto-copie.
        if move_id in {
            "COPYCAT", "MIMIC", "SKETCH", "MIRRORMOVE", "METRONOME",
            "ASSIST", "SLEEPTALK", "INSTRUCT", "MEFIRST",
        }:
            return False
        return str(self.db.move_data(move_id).get("FunctionCode", "None")) != "ProtectUserFromDamagingMovesObstruct"`,
		"Obstruct Assist/Copycat blacklist")
	if err != nil {
		return err
	}
	add("BATTLE-002", "moves", "adapted", "Obstruct escluso dalle mosse richiamabili/copiabili.")

	battle, err = replaceRequired(battle,
		`        code = str(data.get("FunctionCode", "None"))
        healing = ("Heal" in code or "CureTargetStatusHeal" in code or code in {"HealUserFullyAndFallAsleep"})
        if ability == "TRIAGE" and healing:
            priority += 3`,
		`        code = str(data.get("FunctionCode", "None"))
        if code == "HigherPriorityInGrassyTerrain" and self.field.terrain == "grassy":
            priority += 1
        healing = ("Heal" in code or "CureTargetStatusHeal" in code or code in {"HealUserFullyAndFallAsleep"})
        if ability == "TRIAGE" and healing:
            priority += 3`,
		"Grassy Glide priority")
	if err != nil {
		return err
	}
	add("BATTLE-004", "moves", "adapted", "HigherPriorityInGrassyTerrain ottiene +1 solo nel Terreno Erboso.")

	battle, err = replaceRequired(battle,
		`        e_stage = int(self.battle_state(defender)["stages"].get("evasion", 0))
        attacker_ability = self.active_ability(attacker)`,
		`        e_stage = int(self.battle_state(defender)["stages"].get("evasion", 0))
        if code == "IgnoreTargetDefSpDefEvaStatStages":
            e_stage = 0
        attacker_ability = self.active_ability(attacker)`,
		"ignore target evasion")
	if err != nil {
		return err
	}
	add("BATTLE-011", "moves", "adapted", "Chip Away/Darkest Lariat/Sacred Sword ignorano anche l'Elusione.")

	battle, err = replaceRequired(battle,
		`        if status in ("poison", "bad_poison") and (types & {"POISON", "STEEL"} or ability in {"IMMUNITY", "PASTELVEIL"}):
            return False`,
		`        if status in ("poison", "bad_poison"):
            if types & {"POISON", "STEEL"} or ability in {"IMMUNITY", "PASTELVEIL"}:
                return False
            side = self.field.side_of(pokemon)
            if any(int(ally.get("hp", 0)) > 0 and self.field.side_of(ally) == side
                   and self.active_ability(ally) == "PASTELVEIL"
                   for ally in self.field.battlers() if ally is not pokemon):
                return False`,
		"Pastel Veil ally immunity")
	if err != nil {
		return err
	}
	battle, err = replaceRequired(battle,
		`        status = _status(pokemon.get("status"))
        if status == "sleep" and not allow_sleep_action:`,
		`        status = _status(pokemon.get("status"))
        if status in {"poison", "bad_poison"} and self.active_ability(pokemon) == "PASTELVEIL":
            self.cure_status(pokemon)
            status = ""
            messages.append(f"{pokemon['nickname']} guarisce dal veleno grazie a Pastel Veil!")
        if status == "sleep" and not allow_sleep_action:`,
		"Pastel Veil status cure")
	if err != nil {
		return err
	}
	add("BATTLE-009", "abilities", "adapted", "Pastel Veil protegge gli alleati dal veleno e cura il portatore.")

	battle, err = replaceRequired(battle,
		`            if effectiveness > 1: messages.append("È superefficace!")
            elif 0 < effectiveness < 1: messages.append("Non è molto efficace...")`,
		`            if fixed is None:
                if effectiveness > 1: messages.append("È superefficace!")
                elif 0 < effectiveness < 1: messages.append("Non è molto efficace...")`,
		"fixed damage effectiveness message")
	if err != nil {
		return err
	}
	add("BATTLE-010", "moves", "adapted", "Le mosse a danno fisso non mostrano messaggi di efficacia.")

	battle, err = replaceRequired(battle,
		`        if (damage_done > 0 and int(attacker.get("hp", 0)) > 0
                and "HealUserByHalfOfDamageDone" in code and self.can_heal(attacker)):
            maximum=int(attacker.get("stats",{}).get("hp",1)); attacker["hp"]=min(maximum,int(attacker.get("hp",0))+max(1,damage_done//2)); messages.append(f"{attacker['nickname']} assorbe energia!")
        if (damage_done > 0 and int(attacker.get("hp", 0)) > 0
                and "HealUserByThreeQuartersOfDamageDone" in code and self.can_heal(attacker)):
            maximum=int(attacker.get("stats",{}).get("hp",1)); attacker["hp"]=min(maximum,int(attacker.get("hp",0))+max(1,damage_done*3//4)); messages.append(f"{attacker['nickname']} assorbe energia!")`,
		`        if damage_done > 0 and int(attacker.get("hp", 0)) > 0 and "HealUserByHalfOfDamageDone" in code:
            drain=max(1,damage_done//2)
            if self.active_ability(defender) == "LIQUIDOOZE":
                before=int(attacker.get("hp",0)); attacker["hp"]=max(0,before-drain)
                messages.append(f"{attacker['nickname']} subisce l'effetto di Liquid Ooze!")
            elif self.can_heal(attacker):
                maximum=int(attacker.get("stats",{}).get("hp",1)); attacker["hp"]=min(maximum,int(attacker.get("hp",0))+drain); messages.append(f"{attacker['nickname']} assorbe energia!")
        if damage_done > 0 and int(attacker.get("hp", 0)) > 0 and "HealUserByThreeQuartersOfDamageDone" in code:
            drain=max(1,damage_done*3//4)
            if self.active_ability(defender) == "LIQUIDOOZE":
                before=int(attacker.get("hp",0)); attacker["hp"]=max(0,before-drain)
                messages.append(f"{attacker['nickname']} subisce l'effetto di Liquid Ooze!")
            elif self.can_heal(attacker):
                maximum=int(attacker.get("stats",{}).get("hp",1)); attacker["hp"]=min(maximum,int(attacker.get("hp",0))+drain); messages.append(f"{attacker['nickname']} assorbe energia!")`,
		"Liquid Ooze drain")
	if err != nil {
		return err
	}
	add("BATTLE-013", "abilities", "adapted", "Liquid Ooze infligge il danno di drenaggio anche se il bersaglio è andato KO.")

	// Existing native runtime behavior already includes the hotfix semantics.
	add("BATTLE-003", "capture", "native", "La destinazione party/box è decisa dal sistema di cattura PML senza il bypass Ruby forceCatchIntoParty.")
	add("BATTLE-005", "moves", "native", "Eerie Spell riduce i PP solo dopo un colpo che infligge danno.")
	add("BATTLE-006", "battle", "native", "Lo shifting dei battler distanti usa la topologia Python e non l'indice Ruby difettoso.")
	add("BATTLE-007", "ai", "native", "L'AI Python calcola matchup/tipi sul candidato effettivo.")
	add("BATTLE-008", "status", "native", "can_receive_status rifiuta sempre un nuovo status se uno è già presente.")
	add("BATTLE-012", "ui", "native", "Il fight menu PML non dereferenzia una mossa nulla nel renderer non grafico.")

	if err := writeBytesAtomic(battlePath, battle, 0644); err != nil {
		return err
	}

	// ---------------------------------------------------------------------
	// Rare Candy + breeding fixes.
	// ---------------------------------------------------------------------
	itemPath := filepath.Join(dest, "game", "item_effects.py")
	itemData, err := os.ReadFile(itemPath)
	if err != nil {
		return err
	}
	itemData, err = replaceRequired(itemData,
		`        result = gain_experience(root, pokemon, max(0, next_exp-int(pokemon.get("experience", 0))), state)
        return ItemUseResult(True, f"{pokemon['nickname']} sale al livello {pokemon['level']}!", True, evolution=result.get("evolution"))`,
		`        old_level = level
        was_fainted = int(pokemon.get("hp", 0)) <= 0
        result = gain_experience(root, pokemon, max(0, next_exp-int(pokemon.get("experience", 0))), state)
        base_stats = [int(x) for x in str(species.get("BaseStats", "")).replace(" ", "").split(",") if x]
        if was_fainted and int(pokemon.get("level", 1)) > old_level and base_stats and base_stats[0] == 1:
            pokemon["hp"] = 1
        return ItemUseResult(True, f"{pokemon['nickname']} sale al livello {pokemon['level']}!", True, evolution=result.get("evolution"))`,
		"Rare Candy Shedinja")
	if err != nil {
		return err
	}
	if err := writeBytesAtomic(itemPath, itemData, 0644); err != nil {
		return err
	}
	add("MISC-003", "items", "adapted", "Rare Candy porta a 1 PS un Pokémon esausto con Base HP 1 quando sale di livello.")
	add("MISC-004", "evolution", "native", "apply_evolution conserva 0 PS se il Pokémon era esausto.")

	daycarePath := filepath.Join(dest, "game", "daycare_system.py")
	daycare, err := os.ReadFile(daycarePath)
	if err != nil {
		return err
	}
	daycare, err = replaceRequired(daycare,
		`def create_egg(root: Path, state: dict[str, Any], first: dict[str, Any],
               second: dict[str, Any]) -> dict[str, Any]:`,
		`def _inherit_ability_hotfix(root: Path, egg: dict[str, Any], first: dict[str, Any], second: dict[str, Any]) -> None:
    parent = second if _is_ditto(root, first) and not _is_ditto(root, second) else first
    if not _is_ditto(root, first) and not _is_ditto(root, second):
        parent = first if str(first.get("gender", "")).casefold() == "female" else second
    data = registry(root).species_data(egg.get("species", ""))
    regular = [x.strip().upper() for x in str(data.get("Abilities", "")).split(",") if x.strip()]
    hidden = [x.strip().upper() for x in str(data.get("HiddenAbilities", "")).split(",") if x.strip()]
    parent_ability = str(parent.get("ability", "")).upper()
    if parent_ability in hidden:
        if random.randrange(100) < 60:
            egg["ability"] = parent_ability
    elif parent_ability in regular:
        if random.randrange(100) < 80:
            egg["ability"] = parent_ability
        elif len(regular) > 1:
            egg["ability"] = regular[(regular.index(parent_ability) + 1) % 2]


def _inherit_ivs_hotfix(egg: dict[str, Any], first: dict[str, Any], second: dict[str, Any]) -> None:
    stats = list(range(6))
    inherit_count = 5 if str(first.get("held_item") or "").upper() == "DESTINYKNOT" or str(second.get("held_item") or "").upper() == "DESTINYKNOT" else 3
    power_items = {
        "POWERWEIGHT": 0, "POWERBRACER": 1, "POWERBELT": 2,
        "POWERANKLET": 3, "POWERLENS": 4, "POWERBAND": 5,
    }
    forced: dict[int, list[int]] = {}
    for parent in (first, second):
        stat = power_items.get(str(parent.get("held_item") or "").upper())
        if stat is not None:
            ivs = list(parent.get("ivs") or [0] * 6)
            forced.setdefault(stat, []).append(int((ivs + [0] * 6)[stat]))
    egg_ivs = list(egg.get("ivs") or [0] * 6)
    egg_ivs = (egg_ivs + [0] * 6)[:6]
    for stat, values in forced.items():
        egg_ivs[stat] = random.choice(values)
        if stat in stats:
            stats.remove(stat)
        inherit_count -= 1
    for stat in random.sample(stats, min(max(0, inherit_count), len(stats))):
        parent = random.choice((first, second))
        ivs = list(parent.get("ivs") or [0] * 6)
        egg_ivs[stat] = int((ivs + [0] * 6)[stat])
    egg["ivs"] = egg_ivs


def create_egg(root: Path, state: dict[str, Any], first: dict[str, Any],
               second: dict[str, Any]) -> dict[str, Any]:`,
		"breeding hotfix helpers")
	if err != nil {
		return err
	}
	daycare, err = replaceRequired(daycare,
		`    _inherit_moves(root, egg, first, second)
    return egg`,
		`    _inherit_moves(root, egg, first, second)
    _inherit_ability_hotfix(root, egg, first, second)
    _inherit_ivs_hotfix(egg, first, second)
    from game.pokemon_models import recalculate
    recalculate(root, egg)
    return egg`,
		"breeding hotfix integration")
	if err != nil {
		return err
	}
	if err := writeBytesAtomic(daycarePath, daycare, 0644); err != nil {
		return err
	}
	add("MISC-013", "breeding", "adapted", "Eredità abilità 60% Hidden / 80% normale dal genitore corretto.")
	add("MISC-014", "breeding", "adapted", "Eredità IV gestisce Destiny Knot e Power item di entrambi i genitori.")

	// These fixes are already inherent in the Python systems or the original
	// Ruby-specific failure mode does not exist in the PML architecture.
	add("COMPILER-001", "compiler", "not_applicable", "PML non riscrive eventi porta tramite il Compiler Ruby.")
	add("MISC-001", "pc", "native", "Il menu PC PML chiude esplicitamente il menu; non usa l'handler Ruby difettoso.")
	add("MISC-002", "storage", "native", "Le icone box PML sono ricostruite dal modello corrente e non mantengono sprite Ruby stale.")
	add("MISC-005", "battle_intro", "native", "Le intro battaglia PML sono selezionate dal runtime Python.")
	add("MISC-006", "save", "native", "Una nuova partita costruisce un nuovo game_state e azzera il play time.")
	add("MISC-007", "forms", "native", "Le forme leggono metadati in modo nil-safe.")
	add("MISC-008", "mart", "native", "SellPrice assente usa già buy_price//2.")
	add("MISC-009", "party_ui", "native", "La UI party PML gestisce lista vuota senza indice Ruby.")
	add("MISC-010", "ui", "native", "Il sistema help/choice PML gestisce direttamente vita e chiusura delle finestre.")
	add("MISC-011", "text", "native", "Il renderer PML non usa drawSingleFormattedChar Ruby.")
	add("MISC-012", "shadow", "native", "Lo stato Shadow PML conserva/ricostruisce la lista mosse separatamente.")
	add("MISC-015", "roaming", "native", "roaming_system conserva il flag caught tra gli aggiornamenti.")
	add("MISC-016", "audio", "native", "Il manager audio confronta la traccia risolta prima del riavvio.")
	add("OVERWORLD-001", "scene", "not_applicable", "Il framebuffer Pygame viene ridisegnato; non esiste il ghost bitmap di Scene_Map RGSS.")
	add("OVERWORLD-002", "connections", "native", "La profondità bush non ricorre tra Game_Character di mappe collegate.")
	add("OVERWORLD-003", "terrain", "native", "Il terrain tag è risolto dal documento mappa/connessioni PML.")
	add("OVERWORLD-004", "movement", "native", "La velocità del player è stato runtime e può essere modificata dalle route PML.")
	add("OVERWORLD-005", "bug_contest", "not_applicable", "Nessun BugContestState Ruby viene eseguito dal runtime PML.")
	add("OVERWORLD-006", "berries", "native", "Il renderer berry PML deriva lo stadio dal dato persistito.")
	add("OVERWORLD-007", "followers", "native", "following_pokemon usa coordinate/runtime PML e non l'evento follower RGSS.")
	add("OVERWORLD-008", "render", "native", "Il rendering logico 512x384 scala l'intero framebuffer e non altera lo z dei tile priority 1.")
	add("OVERWORLD-009", "camera", "native", "La camera PML mantiene offset/scroll come stato esplicito.")
	add("DEBUG-001", "debug", "not_applicable", "La UI debug PML non usa SpriteWindow_DebugVariables Ruby.")
	add("DEBUG-002", "debug", "not_applicable", "Gli switch debug PML non eval-uano script Ruby nella lista.")
	add("DEBUG-003", "debug", "native", "I test battle PML passano dai loader/modificatori Python.")
	add("DEBUG-004", "debug", "native", "Il modello PML permette form=0 senza il vincolo del menu Ruby.")
	add("DEBUG-005", "debug", "native", "Il debug roaming PML usa stato JSON e non pbDebugRoamers Ruby.")

	coveragePath := filepath.Join(dest, "converted", "essentials_hotfix_coverage.json")
	if err := writeJSON(coveragePath, coverage); err != nil {
		return err
	}
	return nil
}
