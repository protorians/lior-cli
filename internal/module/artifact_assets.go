// Politique des fichiers admis dans une archive `.LiorArtifactPackage`
// (spec docs/specs/applications/module-isolated-runtime.md, §4.4 — audit
// fail-closed de l'archive).
//
// Deux niveaux, appliqués à toutes les entrées (`src/**`, `artifact/**`,
// layout legacy) :
//
//  1. **Exécutables refusés** — d'abord par extension (`.exe`, `.sh`,
//     `.bash`… : un script exécutable n'a rien à faire dans un module),
//     puis par contenu : un binaire ELF, Mach-O ou PE, ou un shebang, est
//     refusé quelle que soit son extension (un payload compilé sans
//     extension ne passe pas).
//  2. **Allowlist d'assets** — un module peut embarquer des médias (images,
//     vidéos, audios, polices) et des fichiers de code/config ; tout autre
//     type est refusé avec la liste des catégories admises. Le socle ne
//     sert d'ailleurs que les extensions de la même allowlist
//     (`@liorian/extended-kit`, ALLOWED_SERVED_EXTENSIONS) : packer un
//     fichier non listé produirait une archive que le socle ne peut pas
//     servir.
package module

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/protorians/lior-cli/internal/i18n"
)

// executableArchiveExts are never packed: executables, scripts and platform
// packages have no place in a module artefact.
var executableArchiveExts = map[string]bool{
	".so": true, ".dylib": true, ".dll": true, ".exe": true,
	".bat": true, ".cmd": true, ".com": true, ".msi": true, ".dmg": true,
	".pkg": true, ".deb": true, ".rpm": true, ".apk": true, ".appimage": true,
	".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".ksh": true,
	".ps1": true, ".psm1": true, ".vbs": true, ".vbe": true, ".wsf": true,
	".py": true, ".pl": true, ".rb": true, ".php": true,
	".jar": true, ".war": true, ".class": true, ".node": true,
	".bin": true, ".run": true, ".app": true, ".ipa": true,
	".o": true, ".obj": true, ".a": true, ".lib": true, ".pdb": true,
}

// allowedArchiveExts is the asset allowlist of an artefact — the code,
// config and media file types a module may embed. It mirrors the served
// extension allowlist of the socle (`@liorian/extended-kit`,
// ALLOWED_SERVED_EXTENSIONS): a file outside this list could never be served
// after installation, so packing it would only defer the rejection.
var allowedArchiveExts = map[string]bool{
	// Code et configuration.
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
	".cjs": true, ".mts": true, ".cts": true,
	".css": true, ".scss": true, ".html": true, ".json": true, ".map": true,
	".svg": true, ".txt": true, ".md": true, ".mdx": true,
	".yaml": true, ".yml": true, ".xml": true, ".csv": true, ".wasm": true,
	// Images.
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".avif": true, ".bmp": true, ".ico": true,
	// Vidéos.
	".mp4": true, ".webm": true, ".mov": true, ".m4v": true,
	// Audios.
	".mp3": true, ".wav": true, ".ogg": true, ".oga": true, ".m4a": true,
	".aac": true, ".flac": true,
	// Polices.
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
}

// executableMagics are the binary signatures of native executables — refused
// whatever the extension (a compiled payload without extension does not
// sneak through).
var executableMagics = [][]byte{
	{0x7F, 'E', 'L', 'F'},    // ELF (Linux)
	{0xFE, 0xED, 0xFA, 0xCE}, // Mach-O 32 bits (macOS)
	{0xFE, 0xED, 0xFA, 0xCF}, // Mach-O 64 bits (macOS)
	{0xCE, 0xFA, 0xED, 0xFE}, // Mach-O 32 bits, byte-swapped
	{0xCF, 0xFA, 0xED, 0xFE}, // Mach-O 64 bits, byte-swapped
	{0xCA, 0xFE, 0xBA, 0xBE}, // Fat/universal binary (macOS)
	{'M', 'Z'},               // PE (Windows)
	{'#', '!'},               // Shebang (scripts)
	{'P', 'K', 0x03, 0x04},   // ZIP imbriqué (payload caché)
}

// checkArchiveEntry applies the whole fail-closed file policy to one file
// about to be packed at archivePath: blocked executable extensions, native
// executable content (magic bytes / shebang), then the asset allowlist.
func checkArchiveEntry(filePath, archivePath string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	if executableArchiveExts[ext] {
		return fmt.Errorf("%s : %s", i18n.T("pack.error.executable_ext"), filePath)
	}
	if err := checkExecutableContent(filePath); err != nil {
		return err
	}
	if ext == "" {
		// Un fichier sans extension (README, LICENSE) ne voyage que s'il est
		// du texte : tout binaire sans extension est un exécutable déguisé.
		return checkTextFile(filePath)
	}
	if !allowedArchiveExts[ext] {
		return fmt.Errorf("%s : %s (%s)", i18n.T("pack.error.asset_extension"), filePath, archivePath)
	}
	return nil
}

// checkExecutableContent reads the head of a file and refuses the native
// executable signatures (ELF, Mach-O, PE, fat binary, shebang).
func checkExecutableContent(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 4)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	for _, magic := range executableMagics {
		if len(magic) > 0 && bytes.HasPrefix(head, magic) {
			return fmt.Errorf("%s : %s", i18n.T("pack.error.executable_content"), filePath)
		}
	}
	return nil
}

// checkTextFile refuses any file that is not plain text — no NUL byte, valid
// UTF-8 — so a binary payload cannot travel under an extensionless name.
func checkTextFile(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	if bytes.IndexByte(data, 0x00) >= 0 {
		return fmt.Errorf("%s : %s", i18n.T("pack.error.executable_content"), filePath)
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("%s : %s", i18n.T("pack.error.executable_content"), filePath)
	}
	return nil
}
