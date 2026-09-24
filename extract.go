package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nwaples/rardecode/v2"
)

// rePart riconosce i volumi RAR multi-parte nuovo stile: name.partNN.rar.
var rePart = regexp.MustCompile(`(?i)\.part0*(\d+)\.rar$`)

// buildPasswords costruisce l'elenco di password da provare in ordine: prima
// nessuna password, poi quella esplicita (-p), poi quelle del file.
func buildPasswords(p, pwfile string) []string {
	out := []string{""}
	seen := map[string]bool{"": true}
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if p != "" {
		add(p)
	}
	if pwfile != "" {
		if data, err := os.ReadFile(pwfile); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(strings.TrimRight(line, "\r"))
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				add(line)
			}
		}
	}
	return out
}

// safeJoin previene il path-traversal (zip-slip) durante l'estrazione.
func safeJoin(base, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := filepath.Clean("/" + name)
	target := filepath.Join(base, clean)
	root := filepath.Clean(base)
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("percorso non sicuro nell'archivio: %q", name)
	}
	return target, nil
}

// goExtractRAR estrae un archivio RAR (anche multi-volume e cifrato) con il
// decoder Go integrato.
func goExtractRAR(archive, dest, pw string) error {
	var opts []rardecode.Option
	if pw != "" {
		opts = append(opts, rardecode.Password(pw))
	}
	rc, err := rardecode.OpenReader(archive, opts...)
	if err != nil {
		return err
	}
	defer rc.Close()
	for {
		hdr, err := rc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		if hdr.IsDir {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// goExtractZIP estrae uno ZIP non cifrato con la stdlib.
func goExtractZIP(archive, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if zf.Flags&0x1 != 0 {
			return fmt.Errorf("zip cifrato non supportato dal decoder interno")
		}
		target, err := safeJoin(dest, zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// goExtract sceglie il decoder interno in base all'estensione.
func goExtract(archive, dest, pw string) error {
	if strings.HasSuffix(strings.ToLower(archive), ".zip") {
		return goExtractZIP(archive, dest)
	}
	return goExtractRAR(archive, dest, pw)
}

// ---- fallback a tool esterno ----

func findExtractor() (tool, path string, err error) {
	for _, c := range []string{"rar", "unrar", "7z", "7za", "7zz"} {
		if p, e := exec.LookPath(c); e == nil {
			return c, p, nil
		}
	}
	return "", "", fmt.Errorf("nessun estrattore esterno")
}

func extractArgs(tool, pw, archive, dest string) []string {
	switch tool {
	case "rar", "unrar":
		p := "-p-"
		if pw != "" {
			p = "-p" + pw
		}
		return []string{"x", "-y", "-o+", p, archive, dest + string(os.PathSeparator)}
	default: // 7z
		p := "-p"
		if pw != "" {
			p = "-p" + pw
		}
		return []string{"x", "-y", p, "-o" + dest, archive}
	}
}

func runExternal(toolPath, tool, archive, dest, pw string) error {
	cmd := exec.Command(toolPath, extractArgs(tool, pw, archive, dest)...)
	if pw != "" { // mostra il progresso solo per il tentativo con password
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// extractSet prova le password con il decoder interno; se nessuna funziona e c'è
// un tool esterno, ritenta con quello. Restituisce password e metodo usati.
func extractSet(archive, dest string, passwords []string) (usedPw, method string, err error) {
	var last error
	for _, pw := range passwords {
		if e := goExtract(archive, dest, pw); e == nil {
			return pw, "integrato", nil
		} else {
			last = e
		}
	}
	if tool, toolPath, e := findExtractor(); e == nil {
		for _, pw := range passwords {
			if runExternal(toolPath, tool, archive, dest, pw) == nil {
				return pw, "esterno (" + tool + ")", nil
			}
		}
	}
	return "", "", fmt.Errorf("estrazione fallita (password errata, parti mancanti o formato non supportato): %v", last)
}

// findArchiveSets trova i volumi "principali" da estrarre in dir.
func findArchiveSets(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var mains []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		low := strings.ToLower(name)
		if strings.HasSuffix(low, ".zip") {
			mains = append(mains, filepath.Join(dir, name))
			continue
		}
		if !strings.HasSuffix(low, ".rar") {
			continue
		}
		if m := rePart.FindStringSubmatch(name); m != nil && m[1] != "1" {
			continue // solo il primo volume del set
		}
		mains = append(mains, filepath.Join(dir, name))
	}
	sort.Strings(mains)
	return mains, nil
}

func archiveBase(name string) string {
	if rePart.MatchString(name) {
		if i := strings.LastIndex(strings.ToLower(name), ".part"); i >= 0 {
			return name[:i]
		}
	}
	low := strings.ToLower(name)
	if strings.HasSuffix(low, ".rar") || strings.HasSuffix(low, ".zip") {
		return name[:len(name)-4]
	}
	return name
}

// removeSet elimina tutti i file del set dopo un'estrazione riuscita.
func removeSet(dir, archive string) {
	base := archiveBase(filepath.Base(archive))
	re := regexp.MustCompile(`(?i)^` + regexp.QuoteMeta(base) + `\.(part\d+\.(?:rar|rev)|rar|r\d+|rev|zip)$`)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() && re.MatchString(e.Name()) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// runExtraction estrae tutti i set di archivi presenti in dir.
func runExtraction(dir, pw, pwfile string, cleanup bool) {
	sets, err := findArchiveSets(dir)
	if err != nil {
		fmt.Printf("estrazione: lettura cartella: %v\n", err)
		return
	}
	if len(sets) == 0 {
		fmt.Printf("estrazione: nessun archivio in %s\n", dir)
		return
	}
	passwords := buildPasswords(pw, pwfile)
	fmt.Printf("\n== estrazione ==\n")
	for _, a := range sets {
		fmt.Printf("• %s\n", filepath.Base(a))
		used, method, err := extractSet(a, dir, passwords)
		if err != nil {
			fmt.Printf("  ✗ %v\n", err)
			continue
		}
		if used == "" {
			fmt.Printf("  ✓ estratto (%s, nessuna password)\n", method)
		} else {
			fmt.Printf("  ✓ estratto (%s, password trovata)\n", method)
		}
		if cleanup {
			removeSet(dir, a)
			fmt.Println("  · archivi del set rimossi")
		}
	}
}
