//go:build ignore

// Command package-release packages a built plugin shared library into the zip
// layout the CLIProxyAPI plugin store installer requires, and writes the
// matching sha256 line.
//
// The installer (internal/pluginstore/install.go, readTargetLibrary) rejects an
// archive unless the target dynamic library sits at the zip ROOT under exactly
// the name "<plugin-id><ext>" (or "<plugin-id>-v<version><ext>"), and unless it
// is the only dynamic library in the archive. Nested paths, absolute paths,
// zip-slip paths, and extra dynamic libraries are all hard failures. Shelling
// out to a `zip` binary is avoided because it is not present on the Windows
// runners and does not produce identical entry metadata across platforms.
//
// The emitted checksum line uses sha256sum format ("<hash>  <filename>") so the
// release job can concatenate every per-platform line into one checksums.txt,
// which ParseChecksums (internal/pluginstore/checksum.go) then reads.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	libraryPath := flag.String("library", "", "path to the built shared library")
	archivePath := flag.String("archive", "", "output zip path")
	checksumPath := flag.String("checksum", "", "output sha256 line path")
	flag.Parse()

	if *libraryPath == "" || *archivePath == "" || *checksumPath == "" {
		fmt.Fprintln(os.Stderr, "package-release: -library, -archive and -checksum are required")
		os.Exit(2)
	}

	if err := run(*libraryPath, *archivePath, *checksumPath); err != nil {
		fmt.Fprintf(os.Stderr, "package-release: %v\n", err)
		os.Exit(1)
	}
}

func run(libraryPath, archivePath, checksumPath string) error {
	libraryData, errRead := os.ReadFile(libraryPath)
	if errRead != nil {
		return fmt.Errorf("read library: %w", errRead)
	}

	archiveData, errBuild := buildArchive(filepath.Base(libraryPath), libraryData)
	if errBuild != nil {
		return errBuild
	}

	if errWrite := os.WriteFile(archivePath, archiveData, 0o644); errWrite != nil {
		return fmt.Errorf("write archive: %w", errWrite)
	}

	sum := sha256.Sum256(archiveData)
	// sha256sum format: hash, two spaces, then the bare archive filename. The
	// installer looks the archive up by name, so the path must not be included.
	line := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(archivePath))
	if errWrite := os.WriteFile(checksumPath, []byte(line), 0o644); errWrite != nil {
		return fmt.Errorf("write checksum: %w", errWrite)
	}

	fmt.Printf("packaged %s (%d bytes) -> %s\n", filepath.Base(libraryPath), len(libraryData), filepath.Base(archivePath))
	return nil
}

func buildArchive(entryName string, libraryData []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)

	header := &zip.FileHeader{
		Name:   entryName, // zip root, no directory prefix
		Method: zip.Deflate,
	}
	header.SetMode(0o755)

	entry, errCreate := writer.CreateHeader(header)
	if errCreate != nil {
		return nil, fmt.Errorf("create zip entry: %w", errCreate)
	}
	if _, errWrite := entry.Write(libraryData); errWrite != nil {
		return nil, fmt.Errorf("write zip entry: %w", errWrite)
	}
	if errClose := writer.Close(); errClose != nil {
		return nil, fmt.Errorf("close zip: %w", errClose)
	}
	return buffer.Bytes(), nil
}
