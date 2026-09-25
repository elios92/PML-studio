//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
	"unsafe"
)

// The converted project must be runnable immediately after import.  The
// embedded runtime is the current PLM/Python engine plus a portable CPython
// distribution.  It is project-agnostic: project assets/data are always copied
// from the selected Essentials source and the project title placeholder is
// personalized during extraction.
//
//go:embed runtime_templates/plm_runtime_core.zip
var plmRuntimeCoreZip []byte

// The game launcher is stored inside plm_runtime_core.zip under
// _plm_templates/PLM_Game_Launcher.exe. Keeping it inside the runtime archive
// avoids a second //go:embed dependency that can be lost/quarantined when a
// source patch is extracted on Windows.
const plmGameLauncherArchivePath = "_plm_templates/PLM_Game_Launcher.exe"

const runtimeTemplateVersion = 2
const runtimeProjectTitlePlaceholder = "__PLM_PROJECT_TITLE__"

type runtimeInstallManifest struct {
	Schema          string `json:"schema"`
	Version         int    `json:"version"`
	TemplateVersion int    `json:"template_version"`
	ProjectName     string `json:"project_name"`
	ReleaseEXE      string `json:"release_exe"`
	DebugEXE        string `json:"debug_exe"`
	InstalledAt     string `json:"installed_at"`
}

func safeGameExecutableBaseName(name string) string {
	name = strings.TrimSpace(name)
	// Keep Windows executable filenames ASCII-stable. In particular, normalize
	// Pokemon spellings with accented/corrupted e (Pokémon/Pokèmon/Pok�mon)
	// to "Pokemon" so Explorer, scripts and launch manifests do not display
	// mojibake or inconsistent filenames.
	replacer := strings.NewReplacer(
		"Pokémon", "Pokemon", "POKÉMON", "POKEMON", "pokémon", "pokemon",
		"Pokèmon", "Pokemon", "POKÈMON", "POKEMON", "pokèmon", "pokemon",
		"Pok�mon", "Pokemon", "POK�MON", "POKEMON", "pok�mon", "pokemon",
	)
	name = replacer.Replace(name)
	// If the source title was already decoded with replacement characters,
	// normalize the common "Pok?mon/Pok�mon" shape without relying on the exact
	// Unicode replacement rune.
	pokemonBroken := regexp.MustCompile(`(?i)pok[^a-zA-Z0-9]mon`)
	name = pokemonBroken.ReplaceAllString(name, "Pokemon")
	if name == "" {
		name = "Pokemon Game"
	}
	// Windows-invalid filename characters and ASCII control characters.
	invalid := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	name = invalid.ReplaceAllString(name, " ")
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, " .")
	if name == "" {
		name = "Pokemon Game"
	}
	upper := strings.ToUpper(name)
	reserved := map[string]bool{
		"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
		"COM6": true, "COM7": true, "COM8": true, "COM9": true,
		"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
		"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	}
	if reserved[upper] {
		name += " Game"
	}
	return name
}

func isRuntimeTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py", ".txt", ".json", ".md", ".ini", ".cfg":
		return true
	default:
		return false
	}
}

func runtimePathInsideDestination(dest, archiveName string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(archiveName))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("percorso runtime non sicuro: %q", archiveName)
	}
	root := filepath.Clean(dest)
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("percorso runtime fuori progetto: %q", archiveName)
	}
	return target, nil
}

func readEmbeddedRuntimeFile(archiveName string) ([]byte, error) {
	if len(plmRuntimeCoreZip) == 0 {
		return nil, fmt.Errorf("template runtime PLM non incorporato")
	}
	zr, err := zip.NewReader(bytes.NewReader(plmRuntimeCoreZip), int64(len(plmRuntimeCoreZip)))
	if err != nil {
		return nil, fmt.Errorf("template runtime PLM non leggibile: %w", err)
	}
	wanted := filepath.ToSlash(archiveName)
	for _, entry := range zr.File {
		if filepath.ToSlash(entry.Name) != wanted {
			continue
		}
		if entry.FileInfo().IsDir() {
			return nil, fmt.Errorf("template runtime %s e' una cartella", wanted)
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("template runtime %s non leggibile: %w", wanted, err)
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("template runtime %s non leggibile: %w", wanted, readErr)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("template runtime %s vuoto", wanted)
		}
		return data, nil
	}
	return nil, fmt.Errorf("template runtime mancante nell'archivio: %s", wanted)
}

func normalizeRuntimeCanonicalPBSPaths(data []byte) []byte {
	replacements := [][2][]byte{
		{[]byte(`root/"PBS"`), []byte(`root/"converted"/"PBS"`)},
		{[]byte(`root / "PBS"`), []byte(`root / "converted" / "PBS"`)},
		{[]byte(`project_root / "PBS"`), []byte(`project_root / "converted" / "PBS"`)},
		{[]byte(`self.project_root / "PBS"`), []byte(`self.project_root / "converted" / "PBS"`)},
	}
	for _, pair := range replacements {
		data = bytes.ReplaceAll(data, pair[0], pair[1])
	}
	return data
}


const runtimeControlsHelpPython = `"""PML controls help using the imported Pokémon Essentials UI asset."""
from __future__ import annotations

from pathlib import Path
from typing import Any
import pygame

from game.ui_assets import UIAssets
from game.options_system import event_action, load_settings, resolved_language


def _font(root: Path, size: int) -> pygame.font.Font:
    for name in ("power green.ttf", "Power Green.ttf", "power clear.ttf"):
        path = root / "assets" / "Fonts" / name
        if path.is_file():
            return pygame.font.Font(str(path), size)
    return pygame.font.Font(None, size)


def _wrap(font: pygame.font.Font, text: str, width: int) -> list[str]:
    words = text.split()
    lines: list[str] = []
    current = ""
    for word in words:
        trial = word if not current else current + " " + word
        if current and font.size(trial)[0] > width:
            lines.append(current)
            current = word
        else:
            current = trial
    if current:
        lines.append(current)
    return lines


def show_controls_help(graphics: Any, project_root: Path) -> None:
    root = Path(project_root)
    assets = UIAssets(root)
    original = assets.image("Controls help/help_bg")
    if original is None:
        raise RuntimeError(
            "UI Essentials mancante: assets/Graphics/Pictures/Controls help/help_bg"
        )

    # The converted project is native PML: fixed legacy key-help content is
    # replaced by a pointer to PML's configurable input settings.
    language = resolved_language(load_settings(root))
    italian = language == "it"
    message = (
        "I comandi di tastiera e controller sono completamente configurabili nelle Impostazioni di gioco."
        if italian else
        "Keyboard and controller controls are fully customizable in Game Settings."
    )

    logical = original.copy()
    text_font = _font(root, 22)
    left = 128
    top = 118
    width = 342
    line_h = text_font.get_height() + 3
    for line in _wrap(text_font, message, width):
        logical.blit(text_font.render(line, True, (72, 72, 72)), (left, top))
        top += line_h

    while True:
        for event in pygame.event.get():
            if event.type == pygame.QUIT:
                raise SystemExit(0)
            if event_action(root, event) in ("confirm", "cancel"):
                return

        screen = graphics.screen
        sw, sh = screen.get_size()
        scale = max(1, int(min(sw / 512.0, sh / 384.0)))
        dw, dh = 512 * scale, 384 * scale
        surface = logical if scale == 1 else pygame.transform.scale(logical, (dw, dh))
        ox, oy = (sw - dw) // 2, (sh - dh) // 2
        screen.fill((0, 0, 0))
        screen.blit(surface, (ox, oy))
        graphics.update()
`

const runtimeNameEntryPython = `from __future__ import annotations
from typing import Any
from pathlib import Path
import pygame
from game.ui_assets import UIAssets
from game.message_system import intl
from game.options_system import event_action, load_settings, resolved_language

MALE_PRESET_NAMES=("Alex","Sam","Nico","Ari","Eli")
FEMALE_PRESET_NAMES=("Maya","Luna","Iris","Zoe","Nina")

MODES=[
"ABCDEFGHIJ ,.KLMNOPQRST '-UVWXYZ     ♂♀             0123456789   ",
"abcdefghij ,.klmnopqrst '-uvwxyz     ♂♀             0123456789   ",
"ÀÁÂÄÃàáâäã ÆæÈÉÊË èéêë  ÇçÌÍÎÏ ìíîï  ŒœÒÓÔÖÕòóôöõ ÑñÙÚÛÜ ùúûü  Ýý",
",.:;…•!?¡¿ ♂♀“”‘’﴾﴿*~_^ ΡΚ@#&%+-×÷/= ΠΜ◎○□△♠♥♦♣★✨  $♈♌♒♐♩♪♫☽☾    "
]
ROWS,COLS=13,5
CTRL={-6:(44,120,2),-5:(106,120,2),-4:(168,120,2),-3:(230,120,2),-2:(314,120,3),-1:(394,120,3)}

def _font(root:Path):
 p=root/'assets'/'Fonts'/'power green.ttf'
 return pygame.font.Font(str(p) if p.is_file() else None,27)

def _txt(dst,font,text,x,y,center=False):
 a=font.render(str(text),True,(160,160,160)); b=font.render(str(text),True,(16,24,32))
 if center:x-=b.get_width()//2
 dst.blit(a,(x+2,y+2));dst.blit(b,(x,y))

def _present(graphics,canvas):
 s=graphics.screen; w,h=s.get_size(); k=max(1,int(min(w/512.0,h/384.0))); dw,dh=512*k,384*k
 img=canvas if k==1 else pygame.transform.scale(canvas,(dw,dh))
 s.fill((0,0,0));s.blit(img,((w-dw)//2,(h-dh)//2));graphics.update()

def _player(scene,canvas,assets):
 sh=assets.image('Naming/icon_shadow')
 if sh: canvas.blit(sh,(66,64))
 try:
  st={'character_name':scene._player_charset(),'pattern':0,'direction':2}; fr=scene._character_frame(st)
  if fr: canvas.blit(fr,(88-fr.get_width()//2,76-fr.get_height()))
 except Exception: pass

def choose_player_name(scene:Any):
 root=Path(scene.project_root)
 language=resolved_language(load_settings(root))
 custom_label="Nome personalizzato" if language=="it" else "Custom name"
 profile=int(scene.game_state.get("player_profile",1) or 1)
 presets=FEMALE_PRESET_NAMES if profile==2 else MALE_PRESET_NAMES
 selected=scene._show_choices([custom_label,*presets])
 if scene.game_state.get("quit_requested"):
  return None
 if selected==0:
  return show_name_entry(scene,None,1,10,"",1)
 if 1<=selected<=len(presets):
  return presets[selected-1]
 return None

def show_name_entry(scene:Any,helptext:str|None=None,minlength:int=1,maxlength:int=10,initial:str='',subject:int=0):
 root=Path(scene.project_root); a=UIAssets(root); bg=a.image('Naming/bg'); controls=a.image('Naming/overlay_controls')
 tabs=[a.image(f'Naming/overlay_tab_{i}') for i in range(1,5)]; curs=[None,a.image('Naming/cursor_1'),a.image('Naming/cursor_2'),a.image('Naming/cursor_3')]
 if not bg or not controls or any(x is None for x in tabs[0:4]) or any(x is None for x in curs[1:]):
  raise RuntimeError('UI Essentials Naming incompleta in assets/Graphics/Pictures/Naming')
 if helptext is None: helptext=intl('Your name?')
 val=str(initial or '')[:maxlength]; mode=0; cur=0; font=_font(root)
 def nonempty(p): return 0<=p<len(MODES[mode]) and MODES[mode][p]!=' '
 while True:
  c=bg.copy()
  if subject==1:_player(scene,c,a)
  _txt(c,font,helptext,160,18)
  for i,ch in enumerate(val):_txt(c,font,ch,166+i*24,54)
  for i in range(maxlength):
   y=78 if i==min(len(val),maxlength-1) else 82;pygame.draw.rect(c,(168,184,184),(162+i*24,y+2,22,4));pygame.draw.rect(c,(16,24,32),(160+i*24,y,22,4))
  tab=tabs[mode].copy()
  for row in range(COLS):
   for col in range(ROWS):
    p=row*ROWS+col; ch=MODES[mode][p] if p<len(MODES[mode]) else ' ';_txt(tab,font,ch,44+col*32,24+row*38,True)
  c.blit(tab,(22,162));c.blit(controls,(16,96))
  icon=a.image('Naming/icon_mode')
  if icon and icon.get_width()>mode*60:c.blit(icon,(44+mode*62,120),(mode*60,0,min(60,icon.get_width()-mode*60),min(44,icon.get_height())))
  if cur<0:x,y,t=CTRL[cur];c.blit(curs[t],(x,y))
  else:c.blit(curs[1],(52+32*(cur%ROWS),180+38*(cur//ROWS)))
  _present(scene.graphics,c)
  for e in pygame.event.get():
   if e.type==pygame.QUIT: scene.game_state['quit_requested']=True; return None
   action=event_action(root,e)
   if action=='cancel':
    if val:val=val[:-1]
    continue
   if e.type==pygame.KEYDOWN and e.key==pygame.K_TAB:mode=(mode+1)%4;continue
   if action in ('left','right','up','down'):
    if cur<0:
     order=[-6,-5,-4,-3,-2,-1];i=order.index(cur)
     if action=='left':cur=order[(i-1)%6]
     elif action=='right':cur=order[(i+1)%6]
     elif action=='down':cur={-6:0,-5:2,-4:4,-3:6,-2:9,-1:11}[cur]
     else:cur={-6:52,-5:54,-4:56,-3:58,-2:61,-1:63}[cur]
    else:
     row,col=divmod(cur,ROWS)
     if action=='left':
      for _ in range(ROWS):
       col=(col-1)%ROWS;p=row*ROWS+col
       if nonempty(p):cur=p;break
     elif action=='right':
      for _ in range(ROWS):
       col=(col+1)%ROWS;p=row*ROWS+col
       if nonempty(p):cur=p;break
     elif action=='up':cur=(-6 if col<=1 else -5 if col<=3 else -4 if col<=5 else -3 if col<=7 else -2 if col<=10 else -1) if row==0 else (row-1)*ROWS+col
     else:cur=(-6 if col<=1 else -5 if col<=3 else -4 if col<=5 else -3 if col<=7 else -2 if col<=10 else -1) if row==COLS-1 else (row+1)*ROWS+col
    continue
   if action=='confirm':
    if cur==-2:val=val[:-1]
    elif cur==-1:
     if len(val)>=minlength:return val
    elif cur in (-6,-5,-4,-3):mode=cur+6
    elif cur>=0 and nonempty(cur):
     if len(val)>=maxlength:val=val[:-1]
     val+=MODES[mode][cur]
     if mode==0 and len(val)==1:mode=1
     if len(val)>=maxlength:cur=-1
`

const runtimeEssentialsUIPython = `from __future__ import annotations
from pathlib import Path
import pygame

BASE_W=512
BASE_H=384

def integer_scale(screen: pygame.Surface) -> int:
    w,h=screen.get_size()
    return max(1,int(min(w/BASE_W,h/BASE_H)))

def viewport(screen: pygame.Surface):
    scale=integer_scale(screen)
    w,h=BASE_W*scale,BASE_H*scale
    return scale,(screen.get_width()-w)//2,(screen.get_height()-h)//2,w,h

def present_logical(screen: pygame.Surface, logical: pygame.Surface, clear=True) -> None:
    scale,x,y,w,h=viewport(screen)
    image=logical if scale==1 else pygame.transform.scale(logical,(w,h))
    if clear:
        screen.fill((0,0,0))
    screen.blit(image,(x,y))

def blit_logical_overlay(screen: pygame.Surface, logical: pygame.Surface) -> None:
    present_logical(screen,logical,False)

def _windowskin_file(root: Path, kind: str, index: int) -> Path | None:
    folder=Path(root)/"assets"/"Graphics"/"Windowskins"
    if not folder.is_dir():
        return None
    number=max(0,int(index))+1
    wanted=(f"speech hgss {number}" if kind=="speech" else f"choice {number}").casefold()
    fallback=("speech hgss 1" if kind=="speech" else "choice 1").casefold()
    found={}
    for path in folder.iterdir():
        if path.is_file() and path.suffix.casefold() in (".png",".bmp",".jpg",".jpeg"):
            found.setdefault(path.stem.casefold(),path)
    return found.get(wanted) or found.get(fallback)

def load_windowskin(root: Path, kind: str, index: int=0) -> pygame.Surface:
    path=_windowskin_file(Path(root),kind,index)
    if path is None:
        raise FileNotFoundError(f"Windowskin Essentials mancante: {kind} {int(index)+1}")
    return pygame.image.load(str(path)).convert_alpha()

def skin_metrics(skin: pygame.Surface):
    w,h=skin.get_size()
    if w in (80,96) and h==48:
        body=(32,16,16,16)
    elif w==80 and h==80:
        body=(32,32,16,16)
    else:
        body=((w-16)//2,(h-16)//2,16,16)
    bx,by,bw,bh=body
    right=max(0,w-(bx+bw))
    bottom=max(0,h-(by+bh))
    return bx,by,right,bottom,body

def _stretch(dst,src,dst_rect,src_rect):
    dx,dy,dw,dh=map(int,dst_rect); sx,sy,sw,sh=map(int,src_rect)
    if dw<=0 or dh<=0 or sw<=0 or sh<=0:return
    part=src.subsurface(pygame.Rect(sx,sy,sw,sh))
    dst.blit(pygame.transform.scale(part,(dw,dh)),(dx,dy))

def _tile(dst,src,dst_rect,src_rect):
    dx,dy,dw,dh=map(int,dst_rect); sx,sy,sw,sh=map(int,src_rect)
    if dw<=0 or dh<=0 or sw<=0 or sh<=0:return
    part=src.subsurface(pygame.Rect(sx,sy,sw,sh))
    yy=0
    while yy<dh:
        xx=0
        ph=min(sh,dh-yy)
        while xx<dw:
            pw=min(sw,dw-xx)
            dst.blit(part,(dx+xx,dy+yy),pygame.Rect(0,0,pw,ph))
            xx+=sw
        yy+=sh

def draw_windowskin(dst: pygame.Surface, skin: pygame.Surface, rect) -> tuple[int,int,int,int]:
    x,y,w,h=map(int,rect)
    left,top,right,bottom,body=skin_metrics(skin)
    bx,by,bw,bh=body
    cx,cy=bx+bw,by+bh
    inner_w=max(0,w-left-right)
    inner_h=max(0,h-top-bottom)
    _stretch(dst,skin,(x+left,y+top,inner_w,inner_h),(bx,by,bw,bh))
    _tile(dst,skin,(x+left,y,inner_w,top),(left,0,bw,top))
    _tile(dst,skin,(x,y+top,left,inner_h),(0,top,left,bh))
    _tile(dst,skin,(x+w-right,y+top,right,inner_h),(cx,top,right,bh))
    _tile(dst,skin,(x+left,y+h-bottom,inner_w,bottom),(left,cy,bw,bottom))
    if left and top: dst.blit(skin,(x,y),pygame.Rect(0,0,left,top))
    if right and top: dst.blit(skin,(x+w-right,y),pygame.Rect(cx,0,right,top))
    if left and bottom: dst.blit(skin,(x,y+h-bottom),pygame.Rect(0,cy,left,bottom))
    if right and bottom: dst.blit(skin,(x+w-right,y+h-bottom),pygame.Rect(cx,cy,right,bottom))
    return left,top,right,bottom
`

const runtimeEssentialsDialoguePython = `"""Dialoghi Pokémon Essentials v20.1 su canvas logico 512x384."""
from __future__ import annotations
from typing import Any
import re
import pygame

from game.dialogue_layout import dialogue_pages
from game.essentials_ui import BASE_W,BASE_H,blit_logical_overlay,draw_windowskin,load_windowskin,skin_metrics
from game.options_system import event_action,load_settings,text_speed_delay

def _font(scene: Any) -> pygame.font.Font:
    path=scene.project_root/"assets"/"Fonts"/"power green.ttf"
    return pygame.font.Font(str(path) if path.is_file() else None,27)

def _pressed_advance(scene: Any,event: pygame.event.Event) -> bool:
    if event.type==pygame.JOYBUTTONDOWN:
        return event_action(scene.project_root,event) in ("confirm","cancel")
    if event.type!=pygame.KEYDOWN or getattr(event,"repeat",False):
        return False
    return event_action(scene.project_root,event) in ("confirm","cancel")

def show_dialogue_with_speed(scene: Any,text: str) -> None:
    raw=str(text)
    linecount=2
    match=re.search(r"\\l\[(\d+)\]",raw,re.I)
    if match:
        linecount=max(1,int(match.group(1)))
    centered="<ac>" in raw.casefold()
    raw=re.sub(r"\\l\[\d+\]","",raw,flags=re.I)
    raw=re.sub(r"\\c\[\d+\]","",raw,flags=re.I)
    raw=re.sub(r"</?ac>","",raw,flags=re.I)
    raw=raw.replace("\\b","").replace("\\r","")

    scene._render_world()
    background=scene.graphics.screen.copy()
    font=_font(scene)
    settings=load_settings(scene.project_root)
    skin=load_windowskin(scene.project_root,"speech",int(settings.get("speech_frame",0) or 0))
    left,top,right,bottom,_=skin_metrics(skin)

    options=scene.game_state.get("message_options",{})
    framed=int(options.get("frame",0))==0
    height=top+bottom+(linecount*32)
    position=int(options.get("position",2))
    y=0 if position==0 else (BASE_H-height)//2 if position==1 else BASE_H-height
    box=pygame.Rect(0,y,BASE_W,height)
    scene.game_state["_last_message_rect"]=[box.x,box.y,box.width,box.height]
    content_width=max(1,BASE_W-left-right-4)
    if centered:
        pages=[raw.split("\n")]
    else:
        pages=dialogue_pages(raw,font,content_width,rows=linecount)
    if not pages:return

    delay=max(0.0,float(text_speed_delay(scene.project_root,scene.game_state)))
    page_index=0
    while page_index<len(pages):
        page=pages[page_index]
        full_text="\n".join(page)
        visible_chars=len(full_text) if delay<=0 else 0
        accumulator=0.0
        while True:
            scene.graphics.screen.blit(background,(0,0))
            logical=pygame.Surface((BASE_W,BASE_H),pygame.SRCALPHA)
            if framed:
                draw_windowskin(logical,skin,box)

            remaining=visible_chars
            main=(80,80,88) if framed else (248,248,248)
            shadow=(160,160,168) if framed else (72,80,88)
            for row,line in enumerate(page):
                take=min(len(line),max(0,remaining))
                shown=line[:take]
                remaining-=len(line)
                if remaining>0:remaining-=1
                if not shown:continue
                rendered_shadow=font.render(shown,True,shadow)
                rendered=font.render(shown,True,main)
                tx=(BASE_W-rendered.get_width())//2 if centered else left+2
                ty=y+top+(row*32)
                logical.blit(rendered_shadow,(tx+2,ty+2))
                logical.blit(rendered,(tx,ty))

            complete=visible_chars>=len(full_text)
            if complete and page_index+1<len(pages):
                px=BASE_W-right-8
                py=y+height-bottom//2
                pygame.draw.polygon(logical,main,[(px-5,py-4),(px+5,py-4),(px,py+2)])

            blit_logical_overlay(scene.graphics.screen,logical)
            dt=scene.graphics.update()

            advance=False
            for event in pygame.event.get():
                if event.type==pygame.QUIT:
                    scene.game_state["quit_requested"]=True
                    return
                if _pressed_advance(scene,event):
                    if not complete:
                        visible_chars=len(full_text)
                    else:
                        advance=True
            if advance:
                page_index+=1
                break
            if not complete and delay>0:
                accumulator+=max(0.0,float(dt))
                step=int(accumulator/delay)
                if step>0:
                    visible_chars=min(len(full_text),visible_chars+step)
                    accumulator-=step*delay
`

const runtimeEssentialsTitleUIPython = `from __future__ import annotations
import pygame

from game.essentials_ui import BASE_W,BASE_H,present_logical
from game.map_scene import load_image
from game.message_system import intl
from game.options_system import event_action

TEXT=(232,232,232)
SHADOW=(136,136,136)
MALE=(56,160,248)
MALE_SHADOW=(56,104,168)
FEMALE=(240,72,88)
FEMALE_SHADOW=(160,64,64)

def _font(scene,size=27):
    path=scene.project_root/"assets"/"Fonts"/"power green.ttf"
    return pygame.font.Font(str(path) if path.is_file() else None,size)

def _text(dst,font,text,x,y,align=0,base=TEXT,shadow=SHADOW):
    text=str(text)
    sh=font.render(text,True,shadow); fg=font.render(text,True,base)
    if align==1:x-=fg.get_width()
    elif align==2:x-=fg.get_width()//2
    dst.blit(sh,(x+2,y+2));dst.blit(fg,(x,y))

def _panel_piece(scene,source):
    piece=pygame.Surface((source[2],source[3]),pygame.SRCALPHA)
    piece.blit(scene.panels,(0,0),source)
    return piece

def _player_frame(scene,state):
    profile=int(state.get("player_profile",1) or 1)
    path=scene.characters.find("trchar001" if profile==2 else "trchar000")
    if not path:return None
    sheet=load_image(path)
    fw,fh=sheet.get_width()//4,sheet.get_height()//4
    frame=pygame.Surface((fw,fh),pygame.SRCALPHA)
    frame.blit(sheet,(0,0),(0,0,fw,fh))
    return frame

def _pokemon_icon(scene,pokemon):
    path=scene.icons.find(str(pokemon.get("species","")))
    if not path:return None
    sheet=load_image(path)
    fw=sheet.get_width()//2
    frame=pygame.Surface((fw,sheet.get_height()),pygame.SRCALPHA)
    frame.blit(sheet,(0,0),(0,0,fw,sheet.get_height()))
    return frame

def draw_load_menu(scene,entries,index,save_data):
    bg=scene.load_background
    logical=bg.copy() if bg.get_size()==(BASE_W,BASE_H) else pygame.transform.scale(bg,(BASE_W,BASE_H))
    font=_font(scene,27)
    y=32
    if save_data:
        selected=index==0
        logical.blit(_panel_piece(scene,(0,222 if selected else 0,408,222)),(48,y))
        state=save_data.get("game_state",{})
        map_id=int(save_data.get("map_id",0) or 0)
        name=str(state.get("player_name",intl("Trainer")))
        map_name=str(scene.map_names.get(str(map_id),{}).get("name",intl("Map {1}",f"{map_id:03d}")))
        _text(logical,font,entries[0][1],80,y+16)
        _text(logical,font,map_name,434,y+16,1)
        profile=int(state.get("player_profile",1) or 1)
        base,shadow=(FEMALE,FEMALE_SHADOW) if profile==2 else (MALE,MALE_SHADOW)
        _text(logical,font,name,160,y+70,0,base,shadow)
        _text(logical,font,intl("Badges:"),80,y+118)
        _text(logical,font,int(state.get("badges",0) or 0),254,y+118,1)
        _text(logical,font,intl("Pokédex:"),80,y+150)
        _text(logical,font,len(state.get("pokedex_seen",[])),254,y+150,1)
        minutes=int(state.get("play_time_seconds",0) or 0)//60
        _text(logical,font,intl("Time:"),80,y+182)
        _text(logical,font,f"{minutes//60}h {minutes%60}m" if minutes>=60 else f"{minutes}m",254,y+182,1)
        player=_player_frame(scene,state)
        if player:logical.blit(player,(112-player.get_width()//2,y+80-player.get_height()//2))
        for i,pokemon in enumerate(state.get("party",[])[:6]):
            icon=_pokemon_icon(scene,pokemon)
            if icon:
                cx=334+66*(i%2);cy=y+80+50*(i//2)
                logical.blit(icon,(cx-icon.get_width()//2,cy-icon.get_height()//2))
        y+=224
        first=1
    else:
        first=0

    for row,(_,label) in enumerate(entries[first:]):
        yy=y+row*48
        selected=index==row+first
        logical.blit(_panel_piece(scene,(0,490 if selected else 444,408,46)),(48,yy))
        _text(logical,font,label,80,yy+14)

    present_logical(scene.graphics.screen,logical)

def show_splash(scene):
    if scene.splash is None:return True
    logical=scene.splash if scene.splash.get_size()==(BASE_W,BASE_H) else pygame.transform.scale(scene.splash,(BASE_W,BASE_H))
    started=pygame.time.get_ticks()
    while pygame.time.get_ticks()-started<1800:
        for event in pygame.event.get():
            if event.type==pygame.QUIT:return False
            if event_action(scene.project_root,event) in ("confirm","cancel"):return True
        present_logical(scene.graphics.screen,logical)
        scene.graphics.update()
    return True

def title_wait(scene):
    background=scene.background if scene.background.get_size()==(BASE_W,BASE_H) else pygame.transform.scale(scene.background,(BASE_W,BASE_H))
    while True:
        for event in pygame.event.get():
            if event.type==pygame.QUIT:return False
            if event_action(scene.project_root,event)=="confirm":return True
        logical=background.copy()
        start=scene.start_image.copy()
        alpha=int((pygame.time.get_ticks()//12)%510)
        start.set_alpha(255-abs(255-alpha))
        logical.blit(start,((BASE_W-start.get_width())//2,322))
        present_logical(scene.graphics.screen,logical)
        scene.graphics.update()
`

const runtimeEssentialsChoiceMethod = `    def _show_choices(self, choices: list[str]) -> int:
        if not choices:
            return -1
        from game.essentials_ui import BASE_W, BASE_H, blit_logical_overlay, draw_windowskin, load_windowskin, skin_metrics
        from game.options_system import event_action, load_settings

        selected = 0
        font_path = self.project_root / "assets" / "Fonts" / "power green.ttf"
        font = pygame.font.Font(str(font_path) if font_path.is_file() else None, 27)
        settings = load_settings(self.project_root)
        skin = load_windowskin(self.project_root, "menu", int(settings.get("menu_frame", 0) or 0))
        left, top, right, bottom, _ = skin_metrics(skin)
        text_width = max(font.size(str(choice))[0] for choice in choices)
        width = min(BASE_W, max(left + right + 1, text_width + left + right + 36))
        height = min(BASE_H, top + bottom + (len(choices) * 32))
        last = self.game_state.get("_last_message_rect")
        if isinstance(last, list) and len(last) == 4:
            mx, my, mw, mh = (int(v) for v in last)
            y = my - height
            if y < 0:
                y = my + mh
                if y + height > BASE_H:
                    y = my - height
            x = mx + mw - width
        else:
            x, y = BASE_W - width, 0
        x = max(0, min(BASE_W - width, x))
        y = max(0, min(BASE_H - height, y))
        background = self.graphics.screen.copy()

        while True:
            for event in pygame.event.get():
                if event.type == pygame.QUIT:
                    self.game_state["quit_requested"] = True
                    return selected
                action = event_action(self.project_root, event)
                if action == "up":
                    selected = (selected - 1) % len(choices)
                elif action == "down":
                    selected = (selected + 1) % len(choices)
                elif action == "confirm":
                    return selected
                elif action == "cancel":
                    cancel = getattr(self, "_choice_cancel_type", 0)
                    if cancel == 5:
                        return 4
                    if 1 <= cancel <= len(choices):
                        return cancel - 1

            self.graphics.screen.blit(background, (0, 0))
            logical = pygame.Surface((BASE_W, BASE_H), pygame.SRCALPHA)
            draw_windowskin(logical, skin, (x, y, width, height))
            for row, choice in enumerate(choices):
                ty = y + top + row * 32
                base, shadow = (80, 80, 88), (160, 160, 168)
                if row == selected:
                    cy = ty + 13
                    pygame.draw.polygon(logical, base, [(x + left + 3, cy - 5), (x + left + 11, cy), (x + left + 3, cy + 5)])
                label = str(choice)
                sh = font.render(label, True, shadow)
                fg = font.render(label, True, base)
                tx = x + left + 18
                logical.blit(sh, (tx + 2, ty + 2))
                logical.blit(fg, (tx, ty))
            blit_logical_overlay(self.graphics.screen, logical)
            self.graphics.update()
`


func replaceRuntimePythonSection(data []byte, startMarker, endMarker, replacement string) ([]byte, error) {
	text := string(data)
	start := strings.Index(text, startMarker)
	if start < 0 {
		return nil, fmt.Errorf("runtime Python: sezione iniziale non trovata: %s", strings.TrimSpace(startMarker))
	}
	endRel := strings.Index(text[start+len(startMarker):], endMarker)
	if endRel < 0 {
		return nil, fmt.Errorf("runtime Python: sezione finale non trovata dopo %s", strings.TrimSpace(startMarker))
	}
	end := start + len(startMarker) + endRel
	return []byte(text[:start] + replacement + text[end:]), nil
}


func patchRuntimeButtonEventScene(data []byte) ([]byte, error) {
	text := string(data)
	marker := `if "pbEventScreen(ButtonEventScene)" in script:`
	pos := strings.Index(text, marker)
	if pos < 0 {
		return nil, fmt.Errorf("runtime map_scene.py: ButtonEventScene non trovata")
	}
	lineStart := strings.LastIndex(text[:pos], "\n") + 1
	indent := text[lineStart:pos]
	// The next statement in the v20.1 runtime is an assignment (show_map = ...),
	// not an if. Stopping at the next "if" used to delete that assignment and
	// later crash with NameError: show_map is not defined.
	boundary := "\n" + indent + "show_map = "
	nextRel := strings.Index(text[pos+len(marker):], boundary)
	if nextRel < 0 {
		return nil, fmt.Errorf("runtime map_scene.py: fine blocco ButtonEventScene/show_map non trovata")
	}
	end := pos + len(marker) + nextRel
	replacement := marker + "\n" + indent + "    from game.controls_help import show_controls_help\n" +
		indent + "    show_controls_help(self.graphics, self.project_root)\n" +
		indent + "    return"
	return []byte(text[:pos] + replacement + text[end:]), nil
}

func convertedEssentialsLanguage(dest string) string {
	data, err := os.ReadFile(filepath.Join(dest, "converted", "messages.json"))
	if err != nil {
		return "en"
	}
	var payload struct {
		DetectedLanguage string `json:"detected_language"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return "en"
	}
	switch strings.ToLower(strings.TrimSpace(payload.DetectedLanguage)) {
	case "it":
		return "it"
	default:
		return "en"
	}
}

func installRuntimeUICompatibilityPatch(dest string) error {
	controlsPath := filepath.Join(dest, "game", "controls_help.py")
	if err := writeBytesAtomic(controlsPath, []byte(runtimeControlsHelpPython), 0644); err != nil {
		return fmt.Errorf("installazione UI controlli PML: %w", err)
	}
	nameEntryPath := filepath.Join(dest, "game", "name_entry_scene.py")
	if err := writeBytesAtomic(nameEntryPath, []byte(runtimeNameEntryPython), 0644); err != nil {
		return fmt.Errorf("installazione UI Naming Essentials: %w", err)
	}
	if err := writeBytesAtomic(filepath.Join(dest, "game", "essentials_ui.py"), []byte(runtimeEssentialsUIPython), 0644); err != nil {
		return fmt.Errorf("installazione renderer UI Essentials: %w", err)
	}
	if err := writeBytesAtomic(filepath.Join(dest, "game", "essentials_title_ui.py"), []byte(runtimeEssentialsTitleUIPython), 0644); err != nil {
		return fmt.Errorf("installazione UI titolo Essentials: %w", err)
	}

	mapPath := filepath.Join(dest, "game", "map_scene.py")
	data, err := os.ReadFile(mapPath)
	if err != nil {
		return fmt.Errorf("lettura runtime map_scene.py: %w", err)
	}
	patched, err := patchRuntimeButtonEventScene(data)
	if err != nil {
		return err
	}
	oldName := []byte("                name = prompt_text(self.graphics, \"Come ti chiami?\", str(self.game_state.get(\"player_name\", \"Alex\")))")
	newName := []byte("                from game.name_entry_scene import choose_player_name\n                name = choose_player_name(self)")
	if !bytes.Contains(patched, oldName) {
		return fmt.Errorf("runtime map_scene.py: pbTrainerName non trovato")
	}
	patched = bytes.Replace(patched, oldName, newName, 1)

	oldEntry := []byte(`        entry = re.search(r"pbSet\((\d+),\s*pbEnterText", script)
        if entry:
            defaults = re.findall(r'"([^"]*)"', script)
            initial = defaults[-1] if defaults else ""
            value = prompt_text(self.graphics, defaults[0] if defaults else "Inserisci il testo", initial)
            if value is not None:
                self.game_state["variables"][entry.group(1)] = value
            return`)
	newEntry := []byte(`        entry = re.search(r"pbSet\((\d+),\s*pbEnterText\((.*)\)\s*\)", script, re.I | re.S)
        if entry:
            from game.name_entry_scene import show_name_entry
            args = entry.group(2)
            quoted = re.findall(r'["\']([^"\']*)["\']', args)
            numeric_args = re.sub(r'(["\']).*?\1', '', args)
            numbers = re.findall(r'(?<![A-Za-z_])-?\d+', numeric_args)
            helptext = quoted[0] if quoted else None
            minlength = int(numbers[0]) if len(numbers) > 0 else 1
            maxlength = int(numbers[1]) if len(numbers) > 1 else 10
            initial = quoted[1] if len(quoted) > 1 else ""
            value = show_name_entry(self, helptext, minlength, maxlength, initial)
            if value is not None:
                self.game_state["variables"][entry.group(1)] = value
            return`)
	if !bytes.Contains(patched, oldEntry) {
		return fmt.Errorf("runtime map_scene.py: pbEnterText non trovato")
	}
	patched = bytes.Replace(patched, oldEntry, newEntry, 1)

	oldMessageJoin := []byte(`                self._show_dialogue(self._format_text(" ".join(parts)))`)
	newMessageJoin := []byte(`                message = parts[0] if parts else ""
                for continuation in parts[1:]:
                    if continuation and not message.endswith(" "):
                        message += " "
                    message += continuation
                self._show_dialogue(self._format_text(message))`)
	if !bytes.Contains(patched, oldMessageJoin) {
		return fmt.Errorf("runtime map_scene.py: concatenazione Show Text non trovata")
	}
	patched = bytes.Replace(patched, oldMessageJoin, newMessageJoin, 1)

	oldPictures := []byte(`    def _draw_pictures(self) -> None:
        sw, sh = self.graphics.screen.get_size()
        for picture_id in sorted(self.pictures):
            picture = self.pictures[picture_id]
            self._update_picture_motion(picture, pygame.time.get_ticks())
            path = self.picture_catalog.find(picture["name"])
            if path is None:
                continue
            source = load_image(path)
            scale_x = sw / 512 * picture.get("zoom_x", 100) / 100
            scale_y = sh / 384 * picture.get("zoom_y", 100) / 100
            image = pygame.transform.smoothscale(
                source,
                (max(1, int(source.get_width() * scale_x)), max(1, int(source.get_height() * scale_y))),
            ).copy()
            image.set_alpha(int(picture.get("opacity", 255)))
            x = int(picture.get("x", 0) * sw / 512)
            y = int(picture.get("y", 0) * sh / 384)
            if picture.get("origin", 0) == 1:
                x -= image.get_width() // 2
                y -= image.get_height() // 2
            self.graphics.screen.blit(image, (x, y))`)
	newPictures := []byte(`    def _draw_pictures(self) -> None:
        sw, sh = self.graphics.screen.get_size()
        ui_scale = max(1, int(min(sw / 512.0, sh / 384.0)))
        viewport_w, viewport_h = 512 * ui_scale, 384 * ui_scale
        viewport_x = (sw - viewport_w) // 2
        viewport_y = (sh - viewport_h) // 2
        for picture_id in sorted(self.pictures):
            picture = self.pictures[picture_id]
            self._update_picture_motion(picture, pygame.time.get_ticks())
            path = self.picture_catalog.find(picture["name"])
            if path is None:
                continue
            source = load_image(path)
            scale_x = ui_scale * picture.get("zoom_x", 100) / 100
            scale_y = ui_scale * picture.get("zoom_y", 100) / 100
            image = pygame.transform.scale(
                source,
                (max(1, int(source.get_width() * scale_x)), max(1, int(source.get_height() * scale_y))),
            ).copy()
            image.set_alpha(int(picture.get("opacity", 255)))
            x = viewport_x + int(picture.get("x", 0) * ui_scale)
            y = viewport_y + int(picture.get("y", 0) * ui_scale)
            if picture.get("origin", 0) == 1:
                x -= image.get_width() // 2
                y -= image.get_height() // 2
            self.graphics.screen.blit(image, (x, y))`)
	if !bytes.Contains(patched, oldPictures) {
		return fmt.Errorf("runtime map_scene.py: renderer Pictures non trovato")
	}
	patched = bytes.Replace(patched, oldPictures, newPictures, 1)

	// RMXP/Essentials Pictures are screen overlays: map tone/day-night affects
	// the world below them, not the Picture itself. helpadventurebg relies on
	// this exact ordering after pbToneChangeAll darkens the map.
	oldPictureOrder := []byte(`        self._draw_pictures()
        tone = self.game_state.get("screen_tone")`)
	newPictureOrder := []byte(`        tone = self.game_state.get("screen_tone")`)
	if !bytes.Contains(patched, oldPictureOrder) {
		return fmt.Errorf("runtime map_scene.py: ordine Pictures/tone non trovato")
	}
	patched = bytes.Replace(patched, oldPictureOrder, newPictureOrder, 1)

	oldWeatherOrder := []byte(`        self._draw_overworld_weather()
        if pygame.time.get_ticks() < int(self.game_state.get("flash_until", 0)):`)
	newWeatherOrder := []byte(`        self._draw_overworld_weather()
        self._draw_pictures()
        if pygame.time.get_ticks() < int(self.game_state.get("flash_until", 0)):`)
	if !bytes.Contains(patched, oldWeatherOrder) {
		return fmt.Errorf("runtime map_scene.py: punto overlay Pictures non trovato")
	}
	patched = bytes.Replace(patched, oldWeatherOrder, newWeatherOrder, 1)

	oldChoiceStart := []byte("    def _show_choices(self, choices: list[str]) -> int:\n        if not choices:\n            return -1\n        selected = 0\n        font_path = self.project_root / \"assets\" / \"Fonts\" / \"power clear.ttf\"")
	newChoiceStart := []byte("    def _show_choices(self, choices: list[str]) -> int:\n        if not choices:\n            return -1\n        from game.options_system import event_action\n        selected = 0\n        sw, sh = self.graphics.screen.get_size()\n        ui_scale = max(1, int(min(sw / 512.0, sh / 384.0)))\n        viewport_w, viewport_h = 512 * ui_scale, 384 * ui_scale\n        viewport_x = (sw - viewport_w) // 2\n        viewport_y = (sh - viewport_h) // 2\n        font_path = self.project_root / \"assets\" / \"Fonts\" / \"power green.ttf\"")
	if !bytes.Contains(patched, oldChoiceStart) {
		return fmt.Errorf("runtime map_scene.py: finestra scelte non trovata")
	}
	patched = bytes.Replace(patched, oldChoiceStart, newChoiceStart, 1)

	oldChoiceInput := []byte(`                if event.type == pygame.KEYDOWN:
                    if event.key == pygame.K_UP:
                        selected = (selected - 1) % len(choices)
                    elif event.key == pygame.K_DOWN:
                        selected = (selected + 1) % len(choices)
                    elif event.key in (pygame.K_RETURN, pygame.K_KP_ENTER, pygame.K_SPACE):
                        return selected
                    elif event.key == pygame.K_ESCAPE:
                        cancel = getattr(self, "_choice_cancel_type", 0)
                        if cancel == 5:
                            return 4
                        if 1 <= cancel <= len(choices):
                            return cancel - 1`)
	newChoiceInput := []byte(`                action = event_action(self.project_root, event)
                if action == "up":
                    selected = (selected - 1) % len(choices)
                elif action == "down":
                    selected = (selected + 1) % len(choices)
                elif action == "confirm":
                    return selected
                elif action == "cancel":
                    cancel = getattr(self, "_choice_cancel_type", 0)
                    if cancel == 5:
                        return 4
                    if 1 <= cancel <= len(choices):
                        return cancel - 1`)
	if !bytes.Contains(patched, oldChoiceInput) {
		return fmt.Errorf("runtime map_scene.py: input finestra scelte non trovato")
	}
	patched = bytes.Replace(patched, oldChoiceInput, newChoiceInput, 1)

	patched = bytes.Replace(patched, []byte(`        font = pygame.font.Font(str(font_path) if font_path.is_file() else None, 27)`), []byte(`        font = pygame.font.Font(str(font_path) if font_path.is_file() else None, 27 * ui_scale)`), 1)
	patched = bytes.Replace(patched, []byte(`            width = max(260, max(font.size(str(choice))[0] for choice in choices) + 65)
            panel = pygame.Rect(self.graphics.screen.get_width() - width - 35, 35, width, 25 + len(choices) * 38)`), []byte(`            width = max(260 * ui_scale, max(font.size(str(choice))[0] for choice in choices) + 65 * ui_scale)
            panel = pygame.Rect(viewport_x + viewport_w - width - 35 * ui_scale, viewport_y + 35 * ui_scale, width, 25 * ui_scale + len(choices) * 38 * ui_scale)`), 1)
	patched = bytes.Replace(patched, []byte(`                y = panel.y + 16 + row * 38`), []byte(`                y = panel.y + 16 * ui_scale + row * 38 * ui_scale`), 1)
	patched = bytes.Replace(patched, []byte(`                    pygame.draw.rect(self.graphics.screen, (90, 155, 210), (panel.x + 12, y - 3, panel.width - 24, 33), border_radius=4)`), []byte(`                    pygame.draw.rect(self.graphics.screen, (90, 155, 210), (panel.x + 12 * ui_scale, y - 3 * ui_scale, panel.width - 24 * ui_scale, 33 * ui_scale), border_radius=4 * ui_scale)`), 1)
	patched = bytes.Replace(patched, []byte(`                self.graphics.screen.blit(font.render(str(choice), True, (25, 35, 50)), (panel.x + 28, y))`), []byte(`                self.graphics.screen.blit(font.render(str(choice), True, (25, 35, 50)), (panel.x + 28 * ui_scale, y))`), 1)

	patched, err = replaceRuntimePythonSection(
		patched,
		"    def _show_choices(self, choices: list[str]) -> int:",
		"\n    def _show_picture(self, parameters: list[Any]) -> None:",
		runtimeEssentialsChoiceMethod,
	)
	if err != nil {
		return err
	}

	if err := writeBytesAtomic(mapPath, patched, 0644); err != nil {
		return fmt.Errorf("aggiornamento UI eventi runtime: %w", err)
	}

	titlePath := filepath.Join(dest, "game", "title_scene.py")
	titleData, err := os.ReadFile(titlePath)
	if err != nil {
		return fmt.Errorf("lettura runtime title_scene.py: %w", err)
	}
	titleData, err = replaceRuntimePythonSection(
		titleData,
		"    def _show_splash(self):",
		"\n    def _title_wait(self):",
		"    def _show_splash(self):\n        from game.essentials_title_ui import show_splash\n        return show_splash(self)\n",
	)
	if err != nil {
		return err
	}
	titleData, err = replaceRuntimePythonSection(
		titleData,
		"    def _title_wait(self):",
		"\n    def _panel_piece(self, area, size):",
		"    def _title_wait(self):\n        from game.essentials_title_ui import title_wait\n        return title_wait(self)\n",
	)
	if err != nil {
		return err
	}
	titleData, err = replaceRuntimePythonSection(
		titleData,
		"    def _draw_load_menu(self,entries,index,save_data):",
		"\n    def _choose(self):",
		"    def _draw_load_menu(self,entries,index,save_data):\n        from game.essentials_title_ui import draw_load_menu\n        return draw_load_menu(self,entries,index,save_data)\n",
	)
	if err != nil {
		return err
	}
	if err := writeBytesAtomic(titlePath, titleData, 0644); err != nil {
		return fmt.Errorf("aggiornamento UI titolo/load Essentials: %w", err)
	}

	dialoguePath := filepath.Join(dest, "game", "options_dialogue.py")
	dialogue, err := os.ReadFile(dialoguePath)
	if err != nil {
		return fmt.Errorf("lettura options_dialogue.py: %w", err)
	}
	oldHeader := []byte("def show_dialogue_with_speed(scene: Any, text: str) -> None:\n    from game.dialogue_layout import dialogue_pages\n\n    background = scene.graphics.screen.copy()\n    font = _font(scene)\n    screen = scene.graphics.screen\n    options = scene.game_state.get(\"message_options\", {})")
	newHeader := []byte("def show_dialogue_with_speed(scene: Any, text: str) -> None:\n    import re\n    from game.dialogue_layout import dialogue_pages\n\n    linecount = 3\n    match = re.search(r\"\\\\l\\[(\\d+)\\]\", str(text), re.I)\n    if match:\n        linecount = max(1, int(match.group(1)))\n    centered = \"<ac>\" in str(text).lower()\n    text = re.sub(r\"\\\\l\\[\\d+\\]\", \"\", str(text), flags=re.I)\n    text = re.sub(r\"\\\\c\\[\\d+\\]\", \"\", text, flags=re.I)\n    text = re.sub(r\"</?ac>\", \"\", text, flags=re.I)\n    text = text.replace(\"\\\\b\", \"\").replace(\"\\\\r\", \"\")\n\n    scene._render_world()\n    background = scene.graphics.screen.copy()\n    screen = scene.graphics.screen\n    ui_scale = max(1, int(min(screen.get_width() / 512.0, screen.get_height() / 384.0)))\n    viewport_w, viewport_h = 512 * ui_scale, 384 * ui_scale\n    viewport_x = (screen.get_width() - viewport_w) // 2\n    viewport_y = (screen.get_height() - viewport_h) // 2\n    font_path = scene.project_root / \"assets\" / \"Fonts\" / \"power green.ttf\"\n    font = pygame.font.Font(str(font_path) if font_path.is_file() else None, 27 * ui_scale)\n    options = scene.game_state.get(\"message_options\", {})")
	if !bytes.Contains(dialogue, oldHeader) {
		return fmt.Errorf("runtime options_dialogue.py: parser messaggi non trovato")
	}
	dialogue = bytes.Replace(dialogue, oldHeader, newHeader, 1)
	dialogue = bytes.Replace(dialogue, []byte("    height = 130"), []byte("    framed = int(options.get(\"frame\", 0)) == 0\n    line_height = max(1, round(31 * ui_scale))\n    height = min(screen.get_height(), max(round(64 * ui_scale), linecount * line_height + (round(32 * ui_scale) if framed else 0)))"), 1)
	dialogue = bytes.Replace(dialogue, []byte("    pages = dialogue_pages(text, font, box.width - 44)"), []byte("    pages = [text.split(\"\\n\")] if centered else dialogue_pages(text, font, box.width - round(44 * ui_scale), rows=linecount)"), 1)
	dialogue = bytes.Replace(dialogue, []byte("    y = (\n        35 if position == 0\n        else (screen.get_height() - height) // 2 if position == 1\n        else screen.get_height() - 165\n    )\n    box = pygame.Rect(35, y, screen.get_width() - 70, height)"), []byte("    y = (\n        viewport_y + 35 * ui_scale if position == 0\n        else viewport_y + (viewport_h - height) // 2 if position == 1\n        else viewport_y + viewport_h - height - 35 * ui_scale\n    )\n    margin = 35 * ui_scale\n    box = pygame.Rect(viewport_x + margin, y, viewport_w - (margin * 2), height)"), 1)
	dialogue = bytes.Replace(dialogue, []byte("            framed = int(options.get(\"frame\", 0)) == 0\n            if framed:"), []byte("            if framed:"), 1)
	oldDraw := []byte("                if not framed:\n                    screen.blit(\n                        font.render(shown, True, (0, 0, 0)),\n                        (box.x + 23, box.y + 21 + row * 31),\n                    )\n                rendered = font.render(\n                    shown, True, (25, 35, 50) if framed else (255, 255, 255)\n                )\n                screen.blit(rendered, (box.x + 22, box.y + 20 + row * 31))")
	newDraw := []byte("                main = (80, 80, 88) if framed else (248, 248, 248)\n                shadow = (160, 160, 168) if framed else (72, 80, 88)\n                rendered_shadow = font.render(shown, True, shadow)\n                rendered = font.render(shown, True, main)\n                tx = box.centerx - rendered.get_width() // 2 if centered else box.x + round(22 * ui_scale)\n                ty = box.y + round(20 * ui_scale) + row * line_height\n                shadow_offset = max(1, round(2 * ui_scale))\n                screen.blit(rendered_shadow, (tx + shadow_offset, ty + shadow_offset))\n                screen.blit(rendered, (tx, ty))")
	if !bytes.Contains(dialogue, oldDraw) {
		return fmt.Errorf("runtime options_dialogue.py: renderer testo non trovato")
	}
	dialogue = bytes.Replace(dialogue, oldDraw, newDraw, 1)
	if err := writeBytesAtomic(dialoguePath, dialogue, 0644); err != nil {
		return fmt.Errorf("aggiornamento dialoghi Essentials: %w", err)
	}
	// The compatibility module above validates that the embedded template still
	// has the expected entry points. The final implementation is rendered on the
	// native 512x384 Essentials canvas and uses the imported Windowskins.
	if err := writeBytesAtomic(dialoguePath, []byte(runtimeEssentialsDialoguePython), 0644); err != nil {
		return fmt.Errorf("installazione renderer dialoghi Essentials: %w", err)
	}

	optionsPath := filepath.Join(dest, "game", "options_system.py")
	optionsData, err := os.ReadFile(optionsPath)
	if err != nil {
		return fmt.Errorf("lettura options_system.py: %w", err)
	}
	sourceLanguage := convertedEssentialsLanguage(dest)
	optionsData = bytes.Replace(optionsData, []byte(`WINDOW_PRESETS = {
    "window_1336": ("Finestra — 1336×1000", (1336, 1000), False),
    "gba_1x": ("GBA 1x — 240×160", (240, 160), False),
    "gba_2x": ("GBA 2x — 480×320", (480, 320), False),
    "gba_3x": ("GBA 3x — 720×480", (720, 480), False),
    "gba_4x": ("GBA 4x — 960×640", (960, 640), False),
    "window_800": ("Finestra — 800×600", (800, 600), False),
    "window_1024": ("Finestra — 1024×768", (1024, 768), False),
    "window_1280": ("Finestra — 1280×960", (1280, 960), False),
    "fullscreen": ("Schermo intero", (0, 0), True),
}`), []byte(`WINDOW_PRESETS = {
    "essentials_1x": ("Essentials 1x — 512×384", (512, 384), False),
    "essentials_2x": ("Essentials 2x — 1024×768", (1024, 768), False),
    "fullscreen": ("Schermo intero", (0, 0), True),
}`), 1)
	optionsData = bytes.Replace(optionsData, []byte(`"window_mode": "window_1336"`), []byte(`"window_mode": "essentials_1x"`), 1)
	optionsData = bytes.Replace(optionsData, []byte(`settings["window_mode"] = "window_1336"`), []byte(`settings["window_mode"] = "essentials_1x"`), 1)
	optionsData = bytes.Replace(optionsData, []byte(`settings.get("window_mode", "window_800")`), []byte(`settings.get("window_mode", "essentials_1x")`), 1)
	optionsData = bytes.Replace(optionsData, []byte(`WINDOW_PRESETS.get(mode, WINDOW_PRESETS["window_1336"])`), []byte(`WINDOW_PRESETS.get(mode, WINDOW_PRESETS["essentials_1x"])`), 1)
	optionsData = bytes.Replace(optionsData, []byte("\"text_entry\": \"keyboard\","), []byte("\"text_entry\": \"cursor\","), 1)
	optionsData = bytes.Replace(optionsData, []byte("\"language\": \"it\","), []byte(fmt.Sprintf("\"language\": %q,", sourceLanguage)), 1)
	optionsData = bytes.Replace(optionsData, []byte(`    return ["it"]`), []byte(fmt.Sprintf(`    return [%q]`, sourceLanguage)), 1)
	optionsData = bytes.Replace(optionsData, []byte(`    value = str(settings.get("language", "it")).casefold()`), []byte(fmt.Sprintf(`    value = str(settings.get("language", %q)).casefold()`, sourceLanguage)), 1)
	optionsData = bytes.Replace(optionsData, []byte(`    return "it" if value == "system" or value not in {"it"} else value`), []byte(fmt.Sprintf(`    return %q if value == "system" or value != %q else value`, sourceLanguage, sourceLanguage)), 1)
	optionsData = bytes.Replace(optionsData, []byte(`    settings["language"] = "it"`), []byte(fmt.Sprintf(`    settings["language"] = %q`, sourceLanguage)), 1)
	if err := writeBytesAtomic(optionsPath, optionsData, 0644); err != nil {
		return fmt.Errorf("aggiornamento Text Entry: %w", err)
	}
	configPath := filepath.Join(dest, "config", "options.json")
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("lettura config/options.json: %w", err)
	}
	var runtimeConfig map[string]any
	if err := json.Unmarshal(configData, &runtimeConfig); err != nil {
		return fmt.Errorf("config/options.json non valido: %w", err)
	}
	runtimeConfig["text_entry"] = "cursor"
	runtimeConfig["window_mode"] = "essentials_1x"
	runtimeConfig["language"] = sourceLanguage
	configData, err = json.MarshalIndent(runtimeConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("serializzazione config/options.json: %w", err)
	}
	configData = append(configData, '\n')
	if err := writeBytesAtomic(configPath, configData, 0644); err != nil {
		return fmt.Errorf("aggiornamento config UI runtime: %w", err)
	}
	return nil
}

func installRuntimeCore(dest, projectName string) (releaseExe, debugExe string, err error) {
	if len(plmRuntimeCoreZip) == 0 {
		return "", "", fmt.Errorf("template runtime PLM non incorporato")
	}
	zr, err := zip.NewReader(bytes.NewReader(plmRuntimeCoreZip), int64(len(plmRuntimeCoreZip)))
	if err != nil {
		return "", "", fmt.Errorf("template runtime PLM non leggibile: %w", err)
	}

	// The converted game executable is the already-compiled native PML Studio
	// runtime binary itself, switched to game-host mode by runtime_install.json.
	// This avoids a second generic launcher process and, critically, removes the
	// dependency on pythonw.exe as the visible/owning game process.
	selfPath, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("eseguibile PML Studio corrente non disponibile: %w", err)
	}
	launcherBytes, err := os.ReadFile(selfPath)
	if err != nil || len(launcherBytes) == 0 {
		return "", "", fmt.Errorf("eseguibile PML Studio corrente non leggibile: %w", err)
	}
	for _, entry := range zr.File {
		archiveName := filepath.ToSlash(entry.Name)
		if strings.HasPrefix(archiveName, "_plm_templates/") {
			continue
		}
		// CPython is hosted in-process by <Nome progetto>.exe. Never publish a
		// pythonw.exe beside the converted game: it would create a second,
		// misleading executable identity and a child process that can hang.
		if strings.EqualFold(filepath.Base(filepath.FromSlash(archiveName)), "pythonw.exe") {
			continue
		}

		target, pathErr := runtimePathInsideDestination(dest, entry.Name)
		if pathErr != nil {
			return "", "", pathErr
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", "", err
		}
		rc, openErr := entry.Open()
		if openErr != nil {
			return "", "", openErr
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return "", "", readErr
		}
		if isRuntimeTextFile(target) && bytes.Contains(data, []byte(runtimeProjectTitlePlaceholder)) {
			data = bytes.ReplaceAll(data, []byte(runtimeProjectTitlePlaceholder), []byte(projectName))
		}
		// converted/PBS is the single canonical PBS tree for PLM projects.
		// Older runtime templates still referenced a root-level PBS folder,
		// which made New Game fail immediately although conversion had correctly
		// preserved the source PBS under converted/PBS. Normalize those legacy
		// Python paths while installing the embedded runtime; do not duplicate PBS.
		if strings.EqualFold(filepath.Ext(target), ".py") {
			data = normalizeRuntimeCanonicalPBSPaths(data)
		}
		mode := os.FileMode(0644)
		if strings.EqualFold(filepath.Ext(target), ".exe") {
			mode = 0755
		}
		if err := writeBytesAtomic(target, data, mode); err != nil {
			return "", "", err
		}
	}

	// Compatibility patch: preserve the imported Essentials controls UI while
	// routing input configuration to PML's real in-game Options system.
	if err := installRuntimeUICompatibilityPatch(dest); err != nil {
		return "", "", err
	}

	base := safeGameExecutableBaseName(projectName)
	releaseExe = base + ".exe"
	debugExe = base + " DEBUG.exe"
	if len(launcherBytes) == 0 {
		return "", "", fmt.Errorf("launcher Windows PLM mancante nel template runtime (%s)", plmGameLauncherArchivePath)
	}
	if err := writeBytesAtomic(filepath.Join(dest, releaseExe), launcherBytes, 0755); err != nil {
		return "", "", fmt.Errorf("creazione %s: %w", releaseExe, err)
	}
	if err := writeBytesAtomic(filepath.Join(dest, debugExe), launcherBytes, 0755); err != nil {
		return "", "", fmt.Errorf("creazione %s: %w", debugExe, err)
	}

	manifest := runtimeInstallManifest{
		Schema:          "plm.runtime.install",
		Version:         1,
		TemplateVersion: runtimeTemplateVersion,
		ProjectName:     projectName,
		ReleaseEXE:      releaseExe,
		DebugEXE:        debugExe,
		InstalledAt:     time.Now().Format(time.RFC3339),
	}
	if err := writeJSON(filepath.Join(dest, "converted", "runtime_install.json"), manifest); err != nil {
		return "", "", err
	}
	return releaseExe, debugExe, nil
}

func replaceFileAtomicWindows(tmp, path string) error {
	r, _, callErr := pMoveFileExW.Call(
		uintptr(unsafe.Pointer(wstr(tmp))),
		uintptr(unsafe.Pointer(wstr(path))),
		MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH,
	)
	if r == 0 {
		return fmt.Errorf("sostituzione atomica fallita: %v", callErr)
	}
	return nil
}

func writeBytesAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil && !os.IsPermission(err) {
		_ = os.Remove(tmp)
		return err
	}
	// Do not delete a valid destination before publishing the complete temp
	// file. MoveFileExW gives Windows replace-existing semantics and asks the OS
	// to flush the replacement before returning.
	if err := replaceFileAtomicWindows(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func validateLauncherExecution(dest string) error {
	manifestData, err := os.ReadFile(filepath.Join(dest, "converted", "runtime_install.json"))
	if err != nil {
		return fmt.Errorf("manifest runtime mancante: %w", err)
	}
	var manifest runtimeInstallManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("manifest runtime non valido: %w", err)
	}
	// Probe the complete launcher -> bundled Python -> main.py chain without
	// entering the game loop. The launcher forwards command-line arguments, and
	// main.py handles --help in argparse before pygame/scene initialization.
	// PLM_TEST_MODE is kept as an additional signal for launcher versions that
	// support it, but --help is the deterministic termination contract.
	for _, exe := range []string{manifest.ReleaseEXE, manifest.DebugEXE} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, filepath.Join(dest, exe), "--help")
		cmd.Dir = dest
		cmd.Env = append(os.Environ(), "PLM_TEST_MODE=1", "PYTHONUTF8=1")
		output, runErr := cmd.CombinedOutput()
		timedOut := ctx.Err() == context.DeadlineExceeded
		cancel()
		if timedOut {
			return fmt.Errorf("launcher %s non termina il probe --help entro 15 secondi", exe)
		}
		if runErr != nil {
			text := strings.TrimSpace(string(output))
			if text == "" {
				text = runErr.Error()
			}
			return fmt.Errorf("launcher %s non avviabile: %s", exe, text)
		}
	}
	return nil
}

func validateRuntimePythonImports(dest string) error {
	mainPath := filepath.Join(dest, "main.py")
	pythonDLL := filepath.Join(dest, "python311.dll")
	if !exists(pythonDLL) || !exists(mainPath) {
		return fmt.Errorf("runtime Python incorporabile incompleto")
	}
	// PYTHONUTF8 affects Python I/O but does not change how Python 3 decodes a
	// source file that has no encoding declaration. The runtime template can
	// contain legacy Windows-1252 bytes (for example 0xE9 in Italian text), so
	// normalize every Python source to UTF-8 before the import probe.
	if err := normalizeRuntimePythonSourcesUTF8(dest); err != nil {
		return fmt.Errorf("normalizzazione sorgenti Python runtime: %w", err)
	}
	// The executable probe is performed by validateLauncherExecution after the
	// runtime sources are normalized. There is intentionally no pythonw.exe
	// subprocess anymore: CPython is loaded from python311.dll by the project EXE.
	return nil
}

func normalizeRuntimePythonSourcesUTF8(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".py") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if utf8.Valid(data) {
			return nil
		}
		// CP1252 is the legacy encoding used by the Windows-authored runtime
		// templates. Decode the defined 0x80-0x9F characters explicitly and use
		// the byte's Unicode code point for the remaining 0xA0-0xFF range.
		var out strings.Builder
		out.Grow(len(data))
		for _, b := range data {
			if b < 0x80 {
				out.WriteByte(b)
				continue
			}
			if r, ok := cp1252Rune(b); ok {
				out.WriteRune(r)
			} else {
				out.WriteRune(rune(b))
			}
		}
		return writeBytesAtomic(path, []byte(out.String()), 0644)
	})
}

func cp1252Rune(b byte) (rune, bool) {
	switch b {
	case 0x80: return '€', true
	case 0x82: return '‚', true
	case 0x83: return 'ƒ', true
	case 0x84: return '„', true
	case 0x85: return '…', true
	case 0x86: return '†', true
	case 0x87: return '‡', true
	case 0x88: return 'ˆ', true
	case 0x89: return '‰', true
	case 0x8A: return 'Š', true
	case 0x8B: return '‹', true
	case 0x8C: return 'Œ', true
	case 0x8E: return 'Ž', true
	case 0x91: return '‘', true
	case 0x92: return '’', true
	case 0x93: return '“', true
	case 0x94: return '”', true
	case 0x95: return '•', true
	case 0x96: return '–', true
	case 0x97: return '—', true
	case 0x98: return '˜', true
	case 0x99: return '™', true
	case 0x9A: return 'š', true
	case 0x9B: return '›', true
	case 0x9C: return 'œ', true
	case 0x9E: return 'ž', true
	case 0x9F: return 'Ÿ', true
	default: return 0, false
	}
}
func validateRuntimeInstall(dest string) error {
	required := []string{
		"main.py",
		filepath.Join("game", "map_scene.py"),
		filepath.Join("game", "battle_scene.py"),
		filepath.Join("game", "data_registry.py"),
		filepath.Join("config", "options.json"),
		"python311.dll",
		filepath.Join("Lib", "site-packages", "pygame", "__init__.py"),
	}
	for _, rel := range required {
		path := filepath.Join(dest, rel)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			return fmt.Errorf("runtime convertito incompleto: %s", filepath.ToSlash(rel))
		}
	}
	manifestPath := filepath.Join(dest, "converted", "runtime_install.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("manifest runtime mancante: %w", err)
	}
	var manifest runtimeInstallManifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return fmt.Errorf("manifest runtime non valido: %w", err)
	}
	for _, exe := range []string{manifest.ReleaseEXE, manifest.DebugEXE} {
		if strings.TrimSpace(exe) == "" || !exists(filepath.Join(dest, exe)) {
			return fmt.Errorf("launcher progetto mancante: %s", exe)
		}
	}
	if strings.EqualFold(manifest.ReleaseEXE, manifest.DebugEXE) {
		return fmt.Errorf("launcher Release e DEBUG non possono avere lo stesso nome")
	}
	if !strings.Contains(strings.ToUpper(filepath.Base(manifest.DebugEXE)), "DEBUG") {
		return fmt.Errorf("launcher DEBUG non identificabile dal nome: %s", manifest.DebugEXE)
	}
	// Release and DEBUG are copies of the same native PML binary; game-host
	// mode is selected from runtime_install.json and the executable name.
	return nil
}
