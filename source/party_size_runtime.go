//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type partyRuntimeReplacement struct {
	old string
	new string
}

type partyRuntimeFilePatch struct {
	rel          string
	replacements []partyRuntimeReplacement
}

func partySizeRuntimePatches() []partyRuntimeFilePatch {
	return []partyRuntimeFilePatch{
		{"game/storage_system.py", []partyRuntimeReplacement{
			{"from typing import Any\n", "from typing import Any\n\nfrom game.plm_battle_settings import max_party_size\n"},
			{"MAX_PARTY_SIZE = 6", "MAX_PARTY_SIZE = max_party_size()"},
		}},
		{"game/daycare_system.py", []partyRuntimeReplacement{
			{"from game.level_rules import max_level_for_species\n", "from game.level_rules import max_level_for_species\nfrom game.plm_battle_settings import max_party_size\n"},
			{"if len(party) >= 6:", "if len(party) >= max_party_size():"},
		}},
		{"game/event_compat.py", []partyRuntimeReplacement{
			{"from typing import Any\n", "from typing import Any\n\nfrom game.plm_battle_settings import max_party_size\n"},
			{"return len(state.get(\"party\", [])) >= 6 and storage_full(state)", "return len(state.get(\"party\", [])) >= max_party_size() and storage_full(state)"},
		}},
		{"game/pokemon_models.py", []partyRuntimeReplacement{
			{"from game.data_registry import registry, scalar, split_csv\n", "from game.data_registry import registry, scalar, split_csv\nfrom game.plm_battle_settings import max_party_size\n"},
			{"len(state.setdefault(\"party\",[]))<6", "len(state.setdefault(\"party\",[])) < max_party_size()"},
			{"len(state[\"party\"])<6", "len(state[\"party\"]) < max_party_size()"},
			{"len(state[\"party\"]) < 6", "len(state[\"party\"]) < max_party_size()"},
		}},
		{"game/mystery_gift.py", []partyRuntimeReplacement{
			{"from typing import Any\n", "from typing import Any\n\nfrom game.plm_battle_settings import max_party_size\n"},
			{"if len(state[\"party\"]) < 6:", "if len(state[\"party\"]) < max_party_size():"},
		}},
		{"game/load_game_scene.py", []partyRuntimeReplacement{
			{"from game.ui_assets import UIAssets\n", "from game.ui_assets import UIAssets\nfrom game.plm_battle_settings import max_party_size\n"},
			{"f\"Squadra {party}/6    Medaglie {int(state.get('badges', 0))}    \"", "f\"Squadra {party}/{max_party_size(self.root)}    Medaglie {int(state.get('badges', 0))}    \""},
		}},
		{"game/pause_menu.py", []partyRuntimeReplacement{
			{"from game.ui_assets import UIAssets\n", "from game.ui_assets import UIAssets\nfrom game.plm_battle_settings import max_party_size\n"},
			{`        slot_positions=((61,437),(253,437),(454,437),(57,590),(253,592),(454,592))
        for i,p in enumerate(party[:6]):
            sx,sy=slot_positions[i];px,py=self._xy(sx,sy);pw,ph=self._xy(165,120)
            rect=pygame.Rect(px,py,pw,ph);state="panel_round" if i==0 else "panel_rect"
            if i==index:state+="_sel"
            panel=self._ui_image(f"Party/{state}",(rect.width,rect.height),True)
            if panel:screen.blit(panel,rect)
            if i==move_from:pygame.draw.rect(screen,(70,225,185),rect,5,border_radius=7)
            path=self.icons.find(p["species"]);size=min(rect.width,rect.height)
            if path:
                sheet=load_image(path);frame=pygame.Surface((sheet.get_width()//2,sheet.get_height()),pygame.SRCALPHA);frame.blit(sheet,(0,0),(0,0,sheet.get_width()//2,sheet.get_height()));image=pygame.transform.scale(frame,(size,size));screen.blit(image,image.get_rect(center=rect.center))
            hp,maximum=int(p.get("hp",0)),int(p.get("stats",{}).get("hp",1));ix,iy=self._xy(688,106+i*102)
            name=self.small.render(str(p["nickname"]),True,(245,245,245));screen.blit(name,(ix,iy));level=self.small.render(f"Lv.{level_label(p)}",True,(245,245,245));screen.blit(level,(ix+150,iy))
            ratio=max(0,min(1,hp/max(1,maximum)));bar=pygame.Rect(ix+35,iy+42,*self._xy(138,14));hpbg=self._ui_image("Party/overlay_hp_back",bar.size,True)
            if hpbg:screen.blit(hpbg,bar)
            color=(35,185,72) if ratio>.5 else (235,175,40) if ratio>.2 else (215,55,55);pygame.draw.rect(screen,color,(bar.x+30,bar.y+4,int((bar.width-35)*ratio),max(3,bar.height-8)))
            hptext=self.small.render(f"{hp}/{maximum}",True,(245,245,245));screen.blit(hptext,(ix+110,iy+58))
`, `        slot_positions=((61,437),(253,437),(454,437),(57,590),(253,592),(454,592))
        page_size=6;page_start=(max(0,index)//page_size)*page_size
        visible_party=party[page_start:page_start+page_size]
        if len(party)>page_size:
            page_no=page_start//page_size+1;page_count=(len(party)+page_size-1)//page_size
            screen.blit(self.small.render(f"Pagina {page_no}/{page_count}  •  {len(party)}/{max_party_size(self.root)} Pokémon",True,(225,228,232)),self._xy(26,62))
        for local_i,p in enumerate(visible_party):
            i=page_start+local_i
            sx,sy=slot_positions[local_i];px,py=self._xy(sx,sy);pw,ph=self._xy(165,120)
            rect=pygame.Rect(px,py,pw,ph);state="panel_round" if local_i==0 else "panel_rect"
            if i==index:state+="_sel"
            panel=self._ui_image(f"Party/{state}",(rect.width,rect.height),True)
            if panel:screen.blit(panel,rect)
            if i==move_from:pygame.draw.rect(screen,(70,225,185),rect,5,border_radius=7)
            path=self.icons.find(p["species"]);size=min(rect.width,rect.height)
            if path:
                sheet=load_image(path);frame=pygame.Surface((sheet.get_width()//2,sheet.get_height()),pygame.SRCALPHA);frame.blit(sheet,(0,0),(0,0,sheet.get_width()//2,sheet.get_height()));image=pygame.transform.scale(frame,(size,size));screen.blit(image,image.get_rect(center=rect.center))
            hp,maximum=int(p.get("hp",0)),int(p.get("stats",{}).get("hp",1));ix,iy=self._xy(688,106+local_i*102)
            name=self.small.render(str(p["nickname"]),True,(245,245,245));screen.blit(name,(ix,iy));level=self.small.render(f"Lv.{level_label(p)}",True,(245,245,245));screen.blit(level,(ix+150,iy))
            ratio=max(0,min(1,hp/max(1,maximum)));bar=pygame.Rect(ix+35,iy+42,*self._xy(138,14));hpbg=self._ui_image("Party/overlay_hp_back",bar.size,True)
            if hpbg:screen.blit(hpbg,bar)
            color=(35,185,72) if ratio>.5 else (235,175,40) if ratio>.2 else (215,55,55);pygame.draw.rect(screen,color,(bar.x+30,bar.y+4,int((bar.width-35)*ratio),max(3,bar.height-8)))
            hptext=self.small.render(f"{hp}/{maximum}",True,(245,245,245));screen.blit(hptext,(ix+110,iy+58))
`},
		}},
		{"game/battle_scene.py", []partyRuntimeReplacement{
			{"from game.battle_ai import BattleAI\n", "from game.battle_ai import BattleAI\nfrom game.plm_battle_settings import max_party_size\n"},
			{`            for i,pokemon in enumerate(party[:6]):
                col,row=i%2,i//2;rect=pygame.Rect(panel.x+25+col*(panel.width//2),panel.y+25+row*115,panel.width//2-45,96)
                if i==index:pygame.draw.rect(self.graphics.screen,(95,180,218),rect,border_radius=9)
                hp=int(pokemon.get("hp",0));maximum=int(pokemon.get("stats",{}).get("hp",1));color=(70,70,75) if hp>0 else (150,80,80)
                self.graphics.screen.blit(self.small.render(f"{pokemon['nickname']}  Lv.{level_label(pokemon)}",True,color),(rect.x+12,rect.y+13));self.graphics.screen.blit(self.tiny.render(f"PS {hp}/{maximum}",True,color),(rect.x+12,rect.y+53))
`, `            page_size=6;page_start=(index//page_size)*page_size
            for local_i,pokemon in enumerate(party[page_start:page_start+page_size]):
                i=page_start+local_i;col,row=local_i%2,local_i//2;rect=pygame.Rect(panel.x+25+col*(panel.width//2),panel.y+25+row*115,panel.width//2-45,96)
                if i==index:pygame.draw.rect(self.graphics.screen,(95,180,218),rect,border_radius=9)
                hp=int(pokemon.get("hp",0));maximum=int(pokemon.get("stats",{}).get("hp",1));color=(70,70,75) if hp>0 else (150,80,80)
                self.graphics.screen.blit(self.small.render(f"{pokemon['nickname']}  Lv.{level_label(pokemon)}",True,color),(rect.x+12,rect.y+13));self.graphics.screen.blit(self.tiny.render(f"PS {hp}/{maximum}",True,color),(rect.x+12,rect.y+53))
            if len(party)>page_size:
                page_no=page_start//page_size+1;page_count=(len(party)+page_size-1)//page_size
                self.graphics.screen.blit(self.tiny.render(f"Squadra {len(party)}/{max_party_size(self.root)}  •  Pagina {page_no}/{page_count}",True,(70,80,90)),(panel.x+25,panel.bottom-26))
`},
			{`            for i,pokemon in enumerate(party[:6]):
                col,row=i%2,i//2;rect=pygame.Rect(panel.x+25+col*(panel.width//2),panel.y+25+row*115,panel.width//2-45,96)
                if i==index:pygame.draw.rect(self.graphics.screen,(95,180,218),rect,border_radius=9)
                hp=int(pokemon.get("hp",0));maximum=int(pokemon.get("stats",{}).get("hp",1));self.graphics.screen.blit(self.small.render(f"{pokemon['nickname']}  Lv.{level_label(pokemon)}",True,(70,70,75)),(rect.x+12,rect.y+13));self.graphics.screen.blit(self.tiny.render(f"PS {hp}/{maximum}",True,(70,70,75)),(rect.x+12,rect.y+53))
`, `            page_size=6;page_start=(index//page_size)*page_size
            for local_i,pokemon in enumerate(party[page_start:page_start+page_size]):
                i=page_start+local_i;col,row=local_i%2,local_i//2;rect=pygame.Rect(panel.x+25+col*(panel.width//2),panel.y+25+row*115,panel.width//2-45,96)
                if i==index:pygame.draw.rect(self.graphics.screen,(95,180,218),rect,border_radius=9)
                hp=int(pokemon.get("hp",0));maximum=int(pokemon.get("stats",{}).get("hp",1));self.graphics.screen.blit(self.small.render(f"{pokemon['nickname']}  Lv.{level_label(pokemon)}",True,(70,70,75)),(rect.x+12,rect.y+13));self.graphics.screen.blit(self.tiny.render(f"PS {hp}/{maximum}",True,(70,70,75)),(rect.x+12,rect.y+53))
            if len(party)>page_size:
                page_no=page_start//page_size+1;page_count=(len(party)+page_size-1)//page_size
                self.graphics.screen.blit(self.tiny.render(f"Squadra {len(party)}/{max_party_size(self.root)}  •  Pagina {page_no}/{page_count}",True,(70,80,90)),(panel.x+25,panel.bottom-26))
`},
		}},
		{"game/storage_scene.py", []partyRuntimeReplacement{
			{"from game.level_rules import level_label\n", "from game.level_rules import level_label\nfrom game.plm_battle_settings import max_party_size\n"},
			{"def _draw(self,selected_box=None,selected_index=None):", "def _draw(self,selected_box=None,selected_index=None,party_index=None):"},
			{`        for i,p in enumerate(self.state.get("party",[])[:6]):
            r=pygame.Rect(round((17+(i%2)*70)*sx),round((78+(i//2)*76)*sy),round(62*sx),round(64*sy));self._icon(p,r);screen.blit(self.small.render(str(i+1),True,(55,65,75)),(r.x+2,r.y+2))
`, `        party=self.state.get("party",[]);page_size=6;focus=0 if party_index is None else max(0,int(party_index));page_start=(focus//page_size)*page_size
        for local_i,p in enumerate(party[page_start:page_start+page_size]):
            i=page_start+local_i;r=pygame.Rect(round((17+(local_i%2)*70)*sx),round((78+(local_i//2)*76)*sy),round(62*sx),round(64*sy));self._icon(p,r);screen.blit(self.small.render(str(i+1),True,(55,65,75)),(r.x+2,r.y+2))
            if party_index is not None and i==party_index:pygame.draw.rect(screen,(250,224,62),r,3,border_radius=4)
        if len(party)>page_size:
            page_no=page_start//page_size+1;page_count=(len(party)+page_size-1)//page_size
            screen.blit(self.small.render(f"{len(party)}/{max_party_size(self.root)}  P{page_no}/{page_count}",True,(55,65,75)),(left.x+10,left.bottom-24))
`},
			{"self._draw();self.graphics.screen.blit(self.small.render(f\"{title}: {party[idx]['nickname']}\"", "self._draw(party_index=idx);self.graphics.screen.blit(self.small.render(f\"{title}: {party[idx]['nickname']}\""},
		}},
		{"game/title_scene.py", []partyRuntimeReplacement{
			{"from game.ui_assets import UIAssets\n", "from game.ui_assets import UIAssets\nfrom game.plm_battle_settings import max_party_size\n"},
			{`        for i,pokemon in enumerate(state.get("party",[])[:6]):
            path=self.icons.find(str(pokemon.get("species","")))
            if not path:continue
            sheet=load_image(path);frame=pygame.Surface((sheet.get_width()//2,sheet.get_height()),pygame.SRCALPHA);frame.blit(sheet,(0,0),(0,0,sheet.get_width()//2,sheet.get_height()))
            icon=pygame.transform.smoothscale(frame,(74,74));col,row=i%2,i//2;self.graphics.screen.blit(icon,(x+510+col*138,y+112+row*102))
`, `        preview=list(state.get("party",[]))[:max_party_size(self.project_root)]
        compact=len(preview)>6;cols=5 if compact else 2;icon_size=54 if compact else 74;gap_x=58 if compact else 138;gap_y=92 if compact else 102;base_x=x+490 if compact else x+510;base_y=y+126 if compact else y+112
        for i,pokemon in enumerate(preview):
            path=self.icons.find(str(pokemon.get("species","")))
            if not path:continue
            sheet=load_image(path);frame=pygame.Surface((sheet.get_width()//2,sheet.get_height()),pygame.SRCALPHA);frame.blit(sheet,(0,0),(0,0,sheet.get_width()//2,sheet.get_height()))
            icon=pygame.transform.smoothscale(frame,(icon_size,icon_size));col,row=i%cols,i//cols;self.graphics.screen.blit(icon,(base_x+col*gap_x,base_y+row*gap_y))
`},
		}},
		{"game/hall_of_fame.py", []partyRuntimeReplacement{
			{"from game.map_scene import asset_catalog, load_image\n", "from game.map_scene import asset_catalog, load_image\nfrom game.plm_battle_settings import max_party_size\n"},
			{"party = list(self.entry.get(\"party\", []))[:6]", "party = list(self.entry.get(\"party\", []))[:max_party_size(self.root)]"},
			{`            cols = 3
            rows = 2
            cell_w = width // cols
            top = 145
            cell_h = max(180, (height - top - 110) // rows)
`, `            cols = 5 if len(party) > 6 else 3
            rows = max(1, (len(party) + cols - 1) // cols)
            cell_w = width // cols
            top = 145
            cell_h = max(135 if cols == 5 else 180, (height - top - 110) // rows)
`},
			{"sprite = self._pokemon_sprite(pokemon, min(180, cell_h - 55))", "sprite = self._pokemon_sprite(pokemon, min(115 if cols == 5 else 180, cell_h - 55))"},
		}},
	}
}

func applyPartyRuntimeReplacement(content string, r partyRuntimeReplacement) (string, error) {
	// Le patch di importazione inseriscono testo attorno al pattern vecchio:
	// in quel caso riconosci prima la forma già patchata per restare idempotente.
	if strings.Contains(r.new, r.old) && strings.Contains(content, r.new) {
		return content, nil
	}
	// Negli altri casi sostituisci sempre il pattern vecchio se esiste. Patch
	// diverse possono infatti produrre la stessa stringa nuova nello stesso file.
	if strings.Contains(content, r.old) {
		return strings.ReplaceAll(content, r.old, r.new), nil
	}
	if strings.Contains(content, r.new) {
		return content, nil
	}
	return content, fmt.Errorf("pattern runtime non riconosciuto")
}

func validatePartyRuntimePatched(rel, content string) error {
	requireCount := func(marker string, want int) error {
		if got := strings.Count(content, marker); got != want {
			return fmt.Errorf("validazione runtime: %s: marker %q trovato %d volte, atteso %d", rel, marker, got, want)
		}
		return nil
	}
	switch rel {
	case "game/storage_system.py":
		return requireCount("MAX_PARTY_SIZE = max_party_size()", 1)
	case "game/daycare_system.py":
		return requireCount("if len(party) >= max_party_size():", 2)
	case "game/event_compat.py":
		return requireCount("return len(state.get(\"party\", [])) >= max_party_size() and storage_full(state)", 1)
	case "game/pokemon_models.py":
		for _, marker := range []string{
			"len(state.setdefault(\"party\",[])) < max_party_size()",
			"if len(state[\"party\"]) < max_party_size():",
		} {
			if !strings.Contains(content, marker) {
				return fmt.Errorf("validazione runtime: %s: marker mancante %q", rel, marker)
			}
		}
		if strings.Count(content, "if len(state[\"party\"]) < max_party_size():") != 2 {
			return fmt.Errorf("validazione runtime: %s: i controlli party attesi non sono completi", rel)
		}
	case "game/mystery_gift.py":
		return requireCount("if len(state[\"party\"]) < max_party_size():", 1)
	case "game/load_game_scene.py":
		return requireCount("Squadra {party}/{max_party_size(self.root)}", 1)
	case "game/pause_menu.py":
		return requireCount("page_size=6;page_start=(max(0,index)//page_size)*page_size", 1)
	case "game/battle_scene.py":
		return requireCount("page_size=6;page_start=(index//page_size)*page_size", 2)
	case "game/storage_scene.py":
		if !strings.Contains(content, "def _draw(self,selected_box=None,selected_index=None,party_index=None):") ||
			!strings.Contains(content, "page_start=(focus//page_size)*page_size") {
			return fmt.Errorf("validazione runtime: %s: paginazione squadra mancante", rel)
		}
	case "game/title_scene.py":
		return requireCount("preview=list(state.get(\"party\",[]))[:max_party_size(self.project_root)]", 1)
	case "game/hall_of_fame.py":
		return requireCount("party = list(self.entry.get(\"party\", []))[:max_party_size(self.root)]", 1)
	}
	return nil
}

func ensurePartySizeRuntime(root string) error {
	if root == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	patches := partySizeRuntimePatches()
	type pendingWrite struct {
		path string
		data []byte
	}
	pending := make([]pendingWrite, 0, len(patches))
	foundRuntime := false

	// Prima fase: valida e prepara tutto in RAM. Se un file runtime conosciuto
	// non corrisponde alla baseline supportata, non viene scritto nulla.
	for _, patch := range patches {
		path := filepath.Join(root, filepath.FromSlash(patch.rel))
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", patch.rel, err)
		}
		foundRuntime = true
		content := string(raw)
		original := content
		for _, repl := range patch.replacements {
			content, err = applyPartyRuntimeReplacement(content, repl)
			if err != nil {
				return fmt.Errorf("supporto squadra configurabile: %s non è compatibile con la baseline runtime attesa (%w)", patch.rel, err)
			}
		}
		if err := validatePartyRuntimePatched(patch.rel, content); err != nil {
			return err
		}
		if content != original {
			pending = append(pending, pendingWrite{path: path, data: []byte(content)})
		}
	}

	// Il bootstrap minimo creato dal convertitore non contiene ancora il
	// battle runtime completo. In quel caso non c'è niente da patchare qui.
	if !foundRuntime {
		return nil
	}

	backupRoot := filepath.Join(root, ".plm", "backups", "party_size_runtime_v1")
	for _, item := range pending {
		rel, _ := filepath.Rel(root, item.path)
		backup := filepath.Join(backupRoot, rel)
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(backup), 0755); err != nil {
				return err
			}
			current, err := os.ReadFile(item.path)
			if err != nil {
				return err
			}
			if err := os.WriteFile(backup, current, 0644); err != nil {
				return err
			}
		}
	}
	for _, item := range pending {
		tmp := item.path + ".plm.tmp"
		if err := os.WriteFile(tmp, item.data, 0644); err != nil {
			return err
		}
		if err := os.Rename(tmp, item.path); err != nil {
			_ = os.Remove(tmp)
			return err
		}
	}
	return nil
}
