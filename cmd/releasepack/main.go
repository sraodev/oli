// releasepack is maintainer tooling, not part of the installed cleanup binary.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/macho"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const product = "mac-cleanup-studio"

var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type provenance struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuildDate    string `json:"build_date"`
	GoVersion    string `json:"go_version"`
	Architecture string `json:"architecture"`
	Snapshot     bool   `json:"snapshot"`
	Signing      string `json:"signing"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("releasepack", flag.ContinueOnError)
	version := fs.String("version", "", "exact vMAJOR.MINOR.PATCH")
	out := fs.String("out", "", "new artifact directory (must not exist)")
	verify := fs.String("verify", "", "verify an existing artifact directory instead of building")
	snapshot := fs.Bool("snapshot", false, "local rehearsal only: allow untagged/dirty source and mark artifacts non-publishable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !versionPattern.MatchString(*version) {
		return errors.New("releasepack: exact vMAJOR.MINOR.PATCH required")
	}
	if *verify != "" {
		if *out != "" {
			return errors.New("releasepack: choose build or verify")
		}
		return verifyDirectory(*verify, *version, *snapshot)
	}
	if *out == "" {
		return errors.New("releasepack: --out is required")
	}
	return build(*out, *version, *snapshot)
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	data, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, data)
	}
	return strings.TrimSpace(string(data)), nil
}

func build(out, version string, snapshot bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	commit, err := command(ctx, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	status, err := command(ctx, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if !snapshot {
		if status != "" {
			return errors.New("releasepack: release source must be clean; use --snapshot only for local rehearsal")
		}
		tag, err := command(ctx, "git", "rev-parse", "refs/tags/"+version+"^{commit}")
		if err != nil || tag != commit {
			return errors.New("releasepack: release tag must resolve to HEAD")
		}
		kind, err := command(ctx, "git", "cat-file", "-t", "refs/tags/"+version)
		if err != nil || kind != "tag" {
			return errors.New("releasepack: an annotated release tag is required")
		}
	}
	date, err := command(ctx, "git", "show", "-s", "--format=%cI", "HEAD")
	if err != nil {
		return err
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		return errors.New("releasepack: output already exists or is inaccessible")
	}
	stage, err := os.MkdirTemp(filepath.Dir(out), ".mcs-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		return err
	}
	installer, err := os.ReadFile("scripts/install.sh")
	if err != nil {
		return err
	}
	var sums strings.Builder
	for _, arch := range []string{"arm64", "amd64"} {
		meta := provenance{version, commit, date, runtime.Version(), arch, snapshot, "No Developer ID signature; not notarized"}
		binary := filepath.Join(stage, product)
		flags := "-s -w -X main.version=" + version + " -X main.commit=" + commit + " -X main.buildDate=" + date + " -X main.buildIdentity=" + identity(meta)
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false", "-ldflags", flags, "-o", binary, "./cmd/mac-cleanup-studio")
		cmd.Env = append(os.Environ(), "GOOS=darwin", "GOARCH="+arch, "CGO_ENABLED=0", "GOAMD64=v1", "GOARM64=v8.0", "GOEXPERIMENT=", "GOFLAGS=", "GOTOOLCHAIN=local")
		if data, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build %s: %w: %s", arch, err, data)
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			return err
		}
		if err := verifyBinary(data, meta); err != nil {
			return err
		}
		archive, err := pack(data, license, meta)
		if err != nil {
			return err
		}
		name := bundle(version, arch) + ".tar.gz"
		if err := os.WriteFile(filepath.Join(stage, name), archive, 0644); err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%x  %s\n", sha256.Sum256(archive), name)
	}
	if err := os.Remove(filepath.Join(stage, product)); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "install.sh"), installer, 0644); err != nil {
		return err
	}
	fmt.Fprintf(&sums, "%x  install.sh\n", sha256.Sum256(installer))
	if err := os.WriteFile(filepath.Join(stage, "SHA256SUMS"), []byte(sums.String()), 0644); err != nil {
		return err
	}
	if err := verifyDirectory(stage, version, snapshot); err != nil {
		return err
	}
	if !snapshot {
		current, err := command(ctx, "git", "rev-parse", "HEAD")
		if err != nil || current != commit {
			return errors.New("releasepack: source changed during build")
		}
		status, err := command(ctx, "git", "status", "--porcelain")
		if err != nil || status != "" {
			return errors.New("releasepack: source changed during build")
		}
	}
	return os.Rename(stage, out)
}

func bundle(version, arch string) string { return product + "-" + version + "-darwin-" + arch }

func pack(binary, license []byte, meta provenance) ([]byte, error) {
	stamp, err := time.Parse(time.RFC3339, meta.BuildDate)
	if err != nil {
		return nil, err
	}
	manifest, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	gz := gzip.NewWriter(&output)
	tw := tar.NewWriter(gz)
	for _, file := range []struct {
		name string
		mode int64
		data []byte
	}{{product, 0755, binary}, {"LICENSE", 0644, license}, {"RELEASE.json", 0644, append(manifest, '\n')}} {
		h := &tar.Header{Name: bundle(meta.Version, meta.Architecture) + "/" + file.name, Mode: file.mode, Size: int64(len(file.data)), ModTime: stamp.UTC(), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(h); err != nil {
			return nil, err
		}
		if _, err := tw.Write(file.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func verifyBinary(data []byte, meta provenance) error {
	file, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer file.Close()
	want := macho.CpuArm64
	if meta.Architecture == "amd64" {
		want = macho.CpuAmd64
	}
	if file.Cpu != want || file.Type != macho.TypeExec {
		return errors.New("releasepack: wrong Mach-O architecture or file type")
	}
	info, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil {
		return err
	}
	settings := map[string]string{}
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if info.GoVersion != meta.GoVersion || settings["GOOS"] != "darwin" || settings["GOARCH"] != meta.Architecture || settings["CGO_ENABLED"] != "0" {
		return errors.New("releasepack: toolchain/target mismatch")
	}
	// Go omits linker flags from build info under -trimpath. A linked identity
	// also exposed by capabilities binds the archive's version/commit/date.
	if !bytes.Contains(data, []byte(identity(meta))) {
		return errors.New("releasepack: embedded build identity mismatch")
	}
	return nil
}

func identity(meta provenance) string {
	return fmt.Sprintf("mac-cleanup-studio/release/v1:%s:%s:%s:snapshot=%t", meta.Version, meta.Commit, meta.BuildDate, meta.Snapshot)
}

func verifyDirectory(dir, version string, allowSnapshot bool) error {
	data, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	names := []string{bundle(version, "arm64") + ".tar.gz", bundle(version, "amd64") + ".tar.gz", "install.sh"}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(names) {
		return errors.New("releasepack: checksum set is incomplete")
	}
	var common *provenance
	for i, name := range names {
		fields := strings.Fields(lines[i])
		if len(fields) != 2 || fields[1] != name {
			return errors.New("releasepack: unexpected checksum name/order")
		}
		hash, err := hex.DecodeString(fields[0])
		if err != nil || len(hash) != 32 {
			return errors.New("releasepack: invalid checksum")
		}
		payload, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		actual := sha256.Sum256(payload)
		if !bytes.Equal(hash, actual[:]) {
			return fmt.Errorf("releasepack: checksum mismatch: %s", name)
		}
		if i == 2 {
			continue
		}
		meta, err := verifyArchive(payload, version, []string{"arm64", "amd64"}[i], allowSnapshot)
		if err != nil {
			return err
		}
		if common != nil && (common.Commit != meta.Commit || common.BuildDate != meta.BuildDate || common.GoVersion != meta.GoVersion || common.Snapshot != meta.Snapshot) {
			return errors.New("releasepack: architectures have inconsistent provenance")
		}
		common = &meta
	}
	return nil
}

func verifyArchive(data []byte, version, arch string, allowSnapshot bool) (provenance, error) {
	var meta provenance
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return meta, err
	}
	defer gz.Close()
	// Bound the whole decompressed stream, including anything after the tar EOF.
	decoded, err := io.ReadAll(io.LimitReader(gz, (128<<20)+1))
	if err != nil {
		return meta, err
	}
	if len(decoded) > 128<<20 {
		return meta, errors.New("releasepack: archive exceeds size limit")
	}
	reader := bytes.NewReader(decoded)
	tr := tar.NewReader(reader)
	files := map[string][]byte{}
	for _, name := range []string{product, "LICENSE", "RELEASE.json"} {
		h, err := tr.Next()
		if err != nil {
			return meta, err
		}
		mode := int64(0644)
		if name == product {
			mode = 0755
		}
		if h.Name != bundle(version, arch)+"/"+name || h.Typeflag != tar.TypeReg || h.Mode != mode || h.Size < 0 || h.Size > 64<<20 || h.Uid != 0 || h.Gid != 0 {
			return meta, errors.New("releasepack: unexpected archive member")
		}
		file, err := io.ReadAll(tr)
		if err != nil {
			return meta, err
		}
		files[name] = file
	}
	if _, err := tr.Next(); err != io.EOF {
		return meta, errors.New("releasepack: trailing archive member")
	}
	if reader.Len() != 0 {
		return meta, errors.New("releasepack: trailing archive data")
	}
	decoder := json.NewDecoder(bytes.NewReader(files["RELEASE.json"]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&meta); err != nil {
		return meta, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return meta, errors.New("releasepack: trailing provenance data")
	}
	if meta.Version != version || meta.Architecture != arch || (meta.Snapshot && !allowSnapshot) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(meta.Commit) {
		return meta, errors.New("releasepack: invalid or snapshot provenance")
	}
	if _, err := time.Parse(time.RFC3339, meta.BuildDate); err != nil {
		return meta, err
	}
	if meta.Signing != "No Developer ID signature; not notarized" {
		return meta, errors.New("releasepack: unsupported signing claim")
	}
	return meta, verifyBinary(files[product], meta)
}
