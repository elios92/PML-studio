package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const essentialsV201PBSProfileID = "pokemon-essentials-v20.1-2022-06-20-pbs"

// essentialsV201PBSFileHashes is generated from the untouched Pokémon Essentials
// v20.1 (2022-06-20) PBS tree supplied as the canonical reference. Only relative
// paths and SHA-256 fingerprints are stored; no PBS contents are embedded.
var essentialsV201PBSFileHashes = map[string]string{
	"abilities.txt":                 "891c0e20ff3b788da2417176411fd6ba442e8b009e1cad71313f9a0854a1fd23",
	"battle_facility_lists.txt":     "017973304738539fb70eddf59cfdb9c681f38e33a937e8d2116689d1a2d958a5",
	"battle_tower_pokemon.txt":      "38701b2d12c47f4f2e2e8ec50847f9017dcde489738153ff52636779655d71a1",
	"battle_tower_trainers.txt":     "83ba37439ac555a155c52d2fccc13d6e52ecc990b8d916e6d496dcbfaa102bf0",
	"berry_plants.txt":              "252533379b8a94d5f8caed2fc9f9e005ea698ea85ac83abbd6b9cf8f559688eb",
	"cup_fancy_pkmn.txt":            "05af3057a9e44441d8b6dae76631558ff9fae31d053ef5863aa84ed3072e7ade",
	"cup_fancy_pkmn_single.txt":     "866cc9c6569698cd778f70ae2536306a27674e251e8da28cf69ae36099805367",
	"cup_fancy_trainers.txt":        "78bc59ca83d4ebd29630a662d943a6f10aee467ba65757ea445708cdc3655b7f",
	"cup_fancy_trainers_single.txt": "6f50978e6b6120caaadae8b86f32c219567c6e6a6d4cbe301b813e4ee242f252",
	"cup_little_pkmn.txt":           "959f36c793478ae06eccc19a26cb72229d28200255d4704d321fb2c8b4edf135",
	"cup_little_trainers.txt":       "bb497bdb21e02be67c3b8832cd5e69692c9802d24f10bdd0f5c6fde55bc3fb79",
	"cup_pika_pkmn.txt":             "8a9f920c905e4d148026634cdd8d45c378d5cade52e1c972c82236fd7a56a71b",
	"cup_pika_trainers.txt":         "b20cf1b2d0ad3e4355d3e9b43f83a0b0a9aada585d3412a7858fb47c94cbe946",
	"cup_poke_pkmn.txt":             "235f1d0b6e8ec101aaa2d59815bbe636e9a9846c6e68295ee5a4bdbfab3fb622",
	"cup_poke_trainers.txt":         "e1c34f4c770f7cf892dafcf0c0e82e1f2bd2f9bdf67c2243cf5fdb1ab5780e70",
	"encounters.txt":                "ccf1c809a9133d74b49e92f5ef14eafe4fa65b2fccf40f57c45dfd08a89e5c6a",
	"Gen 5/abilities.txt":           "3b9cf4c0fcbd8cfb75c0ac23c17035d831e33d55580051e466f1723dc71d74ec",
	"Gen 5/berry_plants.txt":        "316a9b2baeff853e265c568f21582b3003648710ad0b3241ac06f0dab2045b83",
	"Gen 5/encounters.txt":          "14fdd78abfaf65449e99e95d4b2df120903f6d35bb5a5446dedf541e0204c3cf",
	"Gen 5/items.txt":               "c6e35b372a3dd047dfdf69a2902ca755b64bab3b2ca39adc54090af7ee0196db",
	"Gen 5/moves.txt":               "465df084c0eb48a5a42115607258a1796066c87cb3ce2947dba555d15c727f8b",
	"Gen 5/pokemon.txt":             "a4b9a8d572b5aec7f0c13f45c4612b0d93bc097bc258122e0cdd6d038e41353d",
	"Gen 5/pokemon_forms.txt":       "1e1500fd1d7979cc4b7480e05c7cc7230ccc41c5d2b8c70cd4a14ee1b865cbd5",
	"Gen 5/pokemon_metrics.txt":     "da729be328de902fe566b28e172224cddb9f3c643224208c82f216f44937f946",
	"Gen 5/types.txt":               "d42378ec7f4206369df6cba88972e6900467b7a123324c50b3f04c7323123a4c",
	"Gen 6/abilities.txt":           "e9dbf812edd722fff0cb9c3a1dd80731aa139c38ff10b513ce05119845a882c2",
	"Gen 6/berry_plants.txt":        "252533379b8a94d5f8caed2fc9f9e005ea698ea85ac83abbd6b9cf8f559688eb",
	"Gen 6/encounters.txt":          "14fdd78abfaf65449e99e95d4b2df120903f6d35bb5a5446dedf541e0204c3cf",
	"Gen 6/items.txt":               "3e760b73e73519363a3f7daff4453d6b0ba306c19b76c8667aeddadea78af311",
	"Gen 6/moves.txt":               "25152d4903af005570b33cc23422e60b665a388054405e3b11580176c5fd188a",
	"Gen 6/pokemon.txt":             "cec9cc9ee3cf07f9c8c00ad8d0f1bf46768a4bf8a76ca9a5fe2aaae7c1b144ce",
	"Gen 6/pokemon_forms.txt":       "5ddb50a928b51e3cac423648fc73ee9edebebea78f64478f43c95aeff0347811",
	"Gen 6/pokemon_metrics.txt":     "da729be328de902fe566b28e172224cddb9f3c643224208c82f216f44937f946",
	"Gen 6/types.txt":               "9d7a7a3b4a64d0a0fd68beb6aada9790ce33fccbcc4a02cbb06a93e9c4fe8af7",
	"Gen 7/abilities.txt":           "265cb71f5a31510543fee76a77a21c139516fe38a0ef2738f5cbdc4fe4363ff4",
	"Gen 7/berry_plants.txt":        "252533379b8a94d5f8caed2fc9f9e005ea698ea85ac83abbd6b9cf8f559688eb",
	"Gen 7/encounters.txt":          "ccf1c809a9133d74b49e92f5ef14eafe4fa65b2fccf40f57c45dfd08a89e5c6a",
	"Gen 7/items.txt":               "10d367b43b838a08b59f42f4e02f7170f11eb115902e725b7ff6973b7e4a6d80",
	"Gen 7/moves.txt":               "8b8f2bd072856a5e5af50cddd95b7a44426597f73dcfb6219b1bdf7039572096",
	"Gen 7/pokemon.txt":             "9af68c62c660ade703f3b8bc98dcb27c26ef894946881c3bcb3474ed4ce3cb8d",
	"Gen 7/pokemon_forms.txt":       "ed2a897230eadbed741295edfd1f2cb672c2863733d5445cc03a414c52b232c9",
	"Gen 7/pokemon_metrics.txt":     "da729be328de902fe566b28e172224cddb9f3c643224208c82f216f44937f946",
	"Gen 7/types.txt":               "9d7a7a3b4a64d0a0fd68beb6aada9790ce33fccbcc4a02cbb06a93e9c4fe8af7",
	"Gen 8/abilities.txt":           "891c0e20ff3b788da2417176411fd6ba442e8b009e1cad71313f9a0854a1fd23",
	"Gen 8/berry_plants.txt":        "252533379b8a94d5f8caed2fc9f9e005ea698ea85ac83abbd6b9cf8f559688eb",
	"Gen 8/encounters.txt":          "ccf1c809a9133d74b49e92f5ef14eafe4fa65b2fccf40f57c45dfd08a89e5c6a",
	"Gen 8/items.txt":               "63d8d436888e77f7758c6fb7814e78df68512288544a655451bb068b3ba7838b",
	"Gen 8/moves.txt":               "8787c8b5fe09096d5a220cd05813143e8b343a979a6ee426a51ec82f35ec9037",
	"Gen 8/pokemon.txt":             "2b0b9ac4258d87b482385748271c0c27173004fb9e4c1c9a7c287bdc2babb0d7",
	"Gen 8/pokemon_forms.txt":       "5455f075e5694c563ffd0526bad46925be622766dc2c12504a057cd2d4897b40",
	"Gen 8/pokemon_metrics.txt":     "da729be328de902fe566b28e172224cddb9f3c643224208c82f216f44937f946",
	"Gen 8/types.txt":               "9d7a7a3b4a64d0a0fd68beb6aada9790ce33fccbcc4a02cbb06a93e9c4fe8af7",
	"items.txt":                     "63d8d436888e77f7758c6fb7814e78df68512288544a655451bb068b3ba7838b",
	"map_connections.txt":           "99d5902e2806eb63ab6a2422cf4fb6674c47bad592a97374b6103bec9e5500c6",
	"map_metadata.txt":              "479b47690c1c42b6b2beec86ad887370be3e25afd1ffd5c094175c5149d428c2",
	"metadata.txt":                  "e1ec2a5d9175cd8d5632b5de2a98b8b0e1da515d32a1c28a344fae8e95e64521",
	"moves.txt":                     "8787c8b5fe09096d5a220cd05813143e8b343a979a6ee426a51ec82f35ec9037",
	"phone.txt":                     "fb8d8defa64545ffffcf27dd71880b4315f31c2d2d7801eead6905f9d01b55e2",
	"pokemon.txt":                   "2b0b9ac4258d87b482385748271c0c27173004fb9e4c1c9a7c287bdc2babb0d7",
	"pokemon_forms.txt":             "5455f075e5694c563ffd0526bad46925be622766dc2c12504a057cd2d4897b40",
	"pokemon_metrics.txt":           "da729be328de902fe566b28e172224cddb9f3c643224208c82f216f44937f946",
	"regional_dexes.txt":            "11462515486cb5f9d95059e931af129a5ef648b78364cc39fdb7a07abf9b0af0",
	"ribbons.txt":                   "2a540b63cd33682e24895d86af4768a8bd4d1814cd6eb1a9d7c4a1eea1b9b623",
	"shadow_pokemon.txt":            "6d28dfe9013b9be400020cce4778c6281eb12ef3bccd239386cd186fee8cc29c",
	"town_map.txt":                  "08bc6d1f3e6049065b5217b0fff38d40af086a68b634f9d4d9b889f40d240a7a",
	"trainer_types.txt":             "1eb650df0c8045981467b7a7106850d684d896683578553b26d8eae8c3b9a323",
	"trainers.txt":                  "4ca5e008705bb11cdd050d1aa0282abe313f13cbce57405060f937153709f2e7",
	"types.txt":                     "9d7a7a3b4a64d0a0fd68beb6aada9790ce33fccbcc4a02cbb06a93e9c4fe8af7",
}

// EssentialsPBSBaselineAnalysis classifies the imported PBS tree against the
// untouched v20.1 reference. The importer still preserves the source tree 1:1;
// this analysis exists to distinguish stock data from project customizations.
type EssentialsPBSBaselineAnalysis struct {
	ReferenceProfile  string   `json:"reference_profile"`
	Status            string   `json:"status"` // BASE_V20_1 or MODIFIED_V20_1
	BaselineFiles     int      `json:"baseline_files"`
	ExactFiles        int      `json:"exact_files"`
	ExactMatchPercent float64  `json:"exact_match_percent"`
	ModifiedFiles     []string `json:"modified_files,omitempty"`
	MissingFiles      []string `json:"missing_files,omitempty"`
	AddedFiles        []string `json:"added_files,omitempty"`
}

func pbsSHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func pbsRelKey(rel string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(rel)))
}

func analyzeEssentialsPBSV201(root string) (EssentialsPBSBaselineAnalysis, error) {
	out := EssentialsPBSBaselineAnalysis{
		ReferenceProfile: essentialsV201PBSProfileID,
		Status:           "BASE_V20_1",
		BaselineFiles:    len(essentialsV201PBSFileHashes),
	}
	st, err := os.Stat(root)
	if err != nil {
		return out, err
	}
	if !st.IsDir() {
		return out, fmt.Errorf("PBS non e' una cartella: %s", root)
	}

	baseline := make(map[string]struct {
		rel  string
		hash string
	}, len(essentialsV201PBSFileHashes))
	for rel, hash := range essentialsV201PBSFileHashes {
		baseline[pbsRelKey(rel)] = struct {
			rel  string
			hash string
		}{rel: filepath.ToSlash(rel), hash: hash}
	}

	seen := make(map[string]bool, len(baseline))
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		key := pbsRelKey(relSlash)
		ref, known := baseline[key]
		if !known {
			out.AddedFiles = append(out.AddedFiles, relSlash)
			return nil
		}
		seen[key] = true
		got, err := pbsSHA256File(path)
		if err != nil {
			return err
		}
		if strings.EqualFold(got, ref.hash) {
			out.ExactFiles++
		} else {
			out.ModifiedFiles = append(out.ModifiedFiles, ref.rel)
		}
		return nil
	})
	if err != nil {
		return out, err
	}

	for key, ref := range baseline {
		if !seen[key] {
			out.MissingFiles = append(out.MissingFiles, ref.rel)
		}
	}
	sort.Strings(out.ModifiedFiles)
	sort.Strings(out.MissingFiles)
	sort.Strings(out.AddedFiles)
	if out.BaselineFiles > 0 {
		out.ExactMatchPercent = 100 * float64(out.ExactFiles) / float64(out.BaselineFiles)
	}
	if len(out.ModifiedFiles) > 0 || len(out.MissingFiles) > 0 || len(out.AddedFiles) > 0 {
		out.Status = "MODIFIED_V20_1"
	}
	return out, nil
}

// verifyTreeExact is the non-destruction gate used after importing PBS. It
// verifies that every source file exists at the destination byte-for-byte and
// that the destination contains no unexpected extra file.
func verifyTreeExact(srcRoot, dstRoot string) error {
	srcFiles := map[string]string{}
	err := filepath.WalkDir(srcRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		hash, err := pbsSHA256File(path)
		if err != nil {
			return err
		}
		srcFiles[pbsRelKey(rel)] = hash
		return nil
	})
	if err != nil {
		return err
	}

	dstFiles := map[string]string{}
	err = filepath.WalkDir(dstRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dstRoot, path)
		if err != nil {
			return err
		}
		hash, err := pbsSHA256File(path)
		if err != nil {
			return err
		}
		dstFiles[pbsRelKey(rel)] = hash
		return nil
	})
	if err != nil {
		return err
	}

	for rel, srcHash := range srcFiles {
		dstHash, ok := dstFiles[rel]
		if !ok {
			return fmt.Errorf("PBS non riportato nella conversione: %s", filepath.ToSlash(rel))
		}
		if !strings.EqualFold(srcHash, dstHash) {
			return fmt.Errorf("PBS differente dopo la copia 1:1: %s", filepath.ToSlash(rel))
		}
	}
	for rel := range dstFiles {
		if _, ok := srcFiles[rel]; !ok {
			return fmt.Errorf("file PBS inatteso nella destinazione: %s", filepath.ToSlash(rel))
		}
	}
	return nil
}
