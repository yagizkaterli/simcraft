package main

// count-rerun — HRK-046: son merge PR'larin komutlarini pinned head_sha'daki bos
// temp klonda kosar; ayni kosumdan re-run.csv + ozdes markdown tablo uretir.
//
// Girdi: [{"r":"repo","n":12,"t":"...","b":"PR govdesi","art":"beklenen/artifact/yolu"}]
// Cikti: <prefix>.csv, <prefix>.md, <prefix>-out/<repo>_<pr>.run<N>.log
//
// Degismezler (HRK-046 falsifier'lari):
//   * result yalniz PASS | FAIL | DOES-NOT-RESOLVE
//   * PASS = rc 0 + beklenen artifact diskte + BU kosumda yazilmis; rc 0 + bos cikti PASS DEGIL
//   * head_sha = klonlanan sha (receipt blokunda yapisal olarak dogrulanir)
//   * manset sayilar ciktinin ILK satirlari
//   * kimlik/ozel bagimlilik/host yolu engeli = DOES-NOT-RESOLVE + neden; ASLA FAIL

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type PREntry struct {
	Repo string `json:"r"`
	Num  int    `json:"n"`
	Time string `json:"t"`
	Body string `json:"b"`
	Art  string `json:"art,omitempty"`
}

type RunResult struct {
	Repo             string
	PR               int
	HeadSHA          string
	CloneSHA         string
	SHAVerified      bool
	Command          string
	Access           string
	Runs             int
	PerRunValues     []float64
	MedianSeconds    float64
	RC               int
	Result           string
	Artifact         string
	ArtifactBytes    int64
	OutSHA           string
	ReasonUnresolved string
}

const (
	owner      = "yagizkaterli"
	numRuns    = 3
	runTimeout = 45 * time.Minute
)

// cloneParent: her kosum kendi benzersiz gecici kokunu kullanir; boylece ayni
// makinede paralel kosan iki count-rerun birbirinin klonunu silmez.
var cloneParent = ""

var publicRepos = map[string]bool{
	"simcraft": true,
}

// Yerel kosumda canli repoyu/uzak durumu degistiren komutlar kosulmaz:
// bunlar kimlik/yetki engeli sayilir (DOES-NOT-RESOLVE), FAIL degil.
var forbiddenPatterns = []string{
	"gh pr merge", "gh release", "gh repo delete", "gh api -X DELETE",
	"git push", "git merge", "--force", "-merge=true", "--merge",
	"kubectl delete", "kubectl apply", "helm ",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Kullanim: count-rerun <girdi.json> [cikti-onek]\n")
		os.Exit(1)
	}
	inputFile := os.Args[1]
	outPrefix := "re-run"
	if len(os.Args) > 2 {
		outPrefix = os.Args[2]
	}
	runPrivate := os.Getenv("COUNTRERUN_PRIVATE") != "skip"
	cleanBetween := os.Getenv("COUNTRERUN_CLEAN_BETWEEN") == "1"

	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "girdi okunamadi: %v\n", err)
		os.Exit(1)
	}
	var entries []PREntry
	if err := json.Unmarshal(data, &entries); err != nil {
		fmt.Fprintf(os.Stderr, "json cozulemedi: %v\n", err)
		os.Exit(1)
	}

	if root := os.Getenv("COUNTRERUN_CLONE_PARENT"); root != "" {
		if err := os.MkdirAll(root, 0700); err != nil {
			fmt.Fprintf(os.Stderr, "klon dizini acilamadi: %v\n", err)
			os.Exit(1)
		}
		cloneParent = root
	} else {
		root, err := os.MkdirTemp("", "count-rerun-clones-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "klon dizini acilamadi: %v\n", err)
			os.Exit(1)
		}
		cloneParent = root
	}
	defer os.RemoveAll(cloneParent)

	outDir := outPrefix + "-out"
	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "cikti dizini acilamadi: %v\n", err)
		os.Exit(1)
	}

	toolchain := collectToolchain()

	var results []RunResult
	for i, e := range entries {
		r := processEntry(e, outDir, runPrivate, cleanBetween)
		results = append(results, r)
		fmt.Fprintf(os.Stderr, "[%d/%d] %s#%d %s\n", i+1, len(entries), e.Repo, e.Num, r.Result)
	}

	runnable, private, nocommand, pass, fail, dnr := tally(results)

	// Manset sayilar ciktinin ILK satirlari.
	fmt.Printf("kosulabilir=%d private=%d komutsuz=%d\n", runnable, private, nocommand)
	fmt.Printf("PASS=%d FAIL=%d DOES-NOT-RESOLVE=%d\n", pass, fail, dnr)
	fmt.Printf("## Toolchain\n%s", toolchain)
	fmt.Printf("## Receipt\n")
	for _, r := range results {
		fmt.Printf("receipt repo=%s pr=%d head_sha=%s clone_sha=%s sha_match=%t result=%s artifact=%s\n",
			r.Repo, r.PR, r.HeadSHA, r.CloneSHA, r.SHAVerified, r.Result, r.Artifact)
	}

	if err := writeCSV(outPrefix+".csv", toolchain, results, runnable, private, nocommand, pass, fail, dnr); err != nil {
		fmt.Fprintf(os.Stderr, "csv yazilamadi: %v\n", err)
		os.Exit(1)
	}
	if err := writeMD(outPrefix+".md", toolchain, results, runnable, private, nocommand, pass, fail, dnr); err != nil {
		fmt.Fprintf(os.Stderr, "md yazilamadi: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nCSV: %s\nMD: %s\nOut log dizini: %s\n", outPrefix+".csv", outPrefix+".md", outDir)
}

func tally(results []RunResult) (runnable, private, nocommand, pass, fail, dnr int) {
	for _, r := range results {
		switch {
		case r.Command == "" || r.Command == "(komut yok)":
			nocommand++
		case r.Access == "private":
			private++
		default:
			runnable++
		}
		switch r.Result {
		case "PASS":
			pass++
		case "FAIL":
			fail++
		case "DOES-NOT-RESOLVE":
			dnr++
		}
	}
	return
}

func collectToolchain() string {
	var b strings.Builder
	rustc, _ := exec.Command("rustc", "--version").Output()
	goc, _ := exec.Command("go", "version").Output()
	git, _ := exec.Command("git", "--version").Output()
	fmt.Fprintf(&b, "rustc: %s", strings.TrimSpace(string(rustc)))
	fmt.Fprintf(&b, "\ngo: %s", strings.TrimSpace(string(goc)))
	fmt.Fprintf(&b, "\ngit: %s\n", strings.TrimSpace(string(git)))
	return b.String()
}

func processEntry(e PREntry, outDir string, runPrivate, cleanBetween bool) RunResult {
	res := RunResult{Repo: e.Repo, PR: e.Num, Access: accessLevel(e.Repo)}
	res.Command = extractCommand(e.Body)

	sha, err := getHeadSHA(e.Repo, e.Num)
	if err != nil {
		res.Result = "DOES-NOT-RESOLVE"
		res.ReasonUnresolved = fmt.Sprintf("head_sha cozulemedi: %v", err)
		return res
	}
	res.HeadSHA = sha

	if res.Command == "" {
		res.Command = "(komut yok)"
		res.Result = "DOES-NOT-RESOLVE"
		res.ReasonUnresolved = "PR govdesinde komut bulunamadi"
		return res
	}

	if res.Access == "private" && !runPrivate {
		res.Result = "DOES-NOT-RESOLVE"
		res.ReasonUnresolved = "private repo: token yalniz kendi runner'da"
		return res
	}
	if bad := forbiddenCommand(res.Command); bad != "" {
		res.Result = "DOES-NOT-RESOLVE"
		res.ReasonUnresolved = fmt.Sprintf("kimlik/yetki engeli: yasakli komut (%q)", bad)
		return res
	}

	cloneDir := filepath.Join(cloneParent, fmt.Sprintf("%s_%d", e.Repo, e.Num))
	if err := cloneRepo(e.Repo, sha, cloneDir); err != nil {
		res.Result = "DOES-NOT-RESOLVE"
		res.ReasonUnresolved = fmt.Sprintf("clone: %v", err)
		return res
	}
	cloneSHA, err := revParse(cloneDir)
	if err != nil {
		res.Result = "DOES-NOT-RESOLVE"
		res.ReasonUnresolved = fmt.Sprintf("rev-parse: %v", err)
		return res
	}
	res.CloneSHA = cloneSHA
	res.SHAVerified = cloneSHA == sha
	if !res.SHAVerified {
		res.Result = "DOES-NOT-RESOLVE"
		res.ReasonUnresolved = fmt.Sprintf("head_sha(%s) != klonlanan sha(%s)", sha, cloneSHA)
		return res
	}

	if needsBuild, buildCmd := detectBuildStep(cloneDir, res.Command); needsBuild {
		rc, out, err := runCommand(cloneDir, buildCmd)
		if err != nil || rc != 0 {
			res.Result = "DOES-NOT-RESOLVE"
			res.ReasonUnresolved = fmt.Sprintf("bagimlilik/host engeli: build rc=%d: %s", rc, tail(out, 200))
			return res
		}
	}

	outLog := filepath.Join(outDir, fmt.Sprintf("%s_%d.run%d.log", e.Repo, e.Num, numRuns))
	rowStart := time.Now()

	var runTimes []float64
	var lastRC int
	var lastOut string
	for i := range numRuns {
		if i > 0 && cleanBetween {
			cleanBetweenRuns(cloneDir)
		}
		start := time.Now()
		rc, out, err := runCommand(cloneDir, res.Command)
		runTimes = append(runTimes, time.Since(start).Seconds())
		lastRC, lastOut = rc, out
		if err != nil {
			res.Result = "DOES-NOT-RESOLVE"
			res.ReasonUnresolved = fmt.Sprintf("komut hatasi: %v", err)
			return res
		}
	}

	if err := os.WriteFile(outLog, []byte(lastOut), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "out log yazilamadi %s: %v\n", outLog, err)
	}

	res.Runs = numRuns
	res.PerRunValues = runTimes
	res.MedianSeconds = median(runTimes)
	res.RC = lastRC
	res.OutSHA = sha256hex(lastOut)

	artName, artOK, artBytes := resolveArtifact(e, cloneDir, outLog, lastOut, rowStart)
	res.Artifact = artName
	res.ArtifactBytes = artBytes

	switch {
	case lastRC == 0 && artOK && artBytes > 0:
		res.Result = "PASS"
	case lastRC == 0:
		res.Result = "FAIL"
		res.ReasonUnresolved = "rc=0 ama beklenen artifact yok/eskimis/bos (PASS degil)"
	default:
		res.Result = "FAIL"
		res.ReasonUnresolved = fmt.Sprintf("exit code %d", lastRC)
	}
	return res
}

// resolveArtifact: acik "art" yolu (bu kosumda yazilmis olmali) yoksa
// aracin yazdigi komut cikti logu artifact sayilir.
func resolveArtifact(e PREntry, cloneDir, outLog, out string, start time.Time) (string, bool, int64) {
	if e.Art != "" {
		p := filepath.Join(cloneDir, e.Art)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			if info.ModTime().After(start) && info.Size() > 0 {
				return e.Art, true, info.Size()
			}
			return e.Art, false, info.Size()
		}
		return e.Art, false, 0
	}
	info, err := os.Stat(outLog)
	if err != nil {
		return "out-log", false, 0
	}
	return "out-log", info.Size() > 0, info.Size()
}

func accessLevel(repo string) string {
	if publicRepos[repo] {
		return "public"
	}
	return "private"
}

func forbiddenCommand(command string) string {
	low := strings.ToLower(command)
	for _, p := range forbiddenPatterns {
		if strings.Contains(low, strings.ToLower(p)) {
			return p
		}
	}
	return ""
}

func extractCommand(body string) string {
	re := regexp.MustCompile("(?s)```(?:sh|bash|shell|console|cmd)?\\s*\\n(.*?)```")
	if m := re.FindStringSubmatch(body); len(m) >= 2 {
		cmd := strings.TrimSpace(m[1])
		if cmd != "" && !strings.HasPrefix(cmd, "#") {
			return cmd
		}
	}
	// Satir ici adaylar: komut+arguman iceren en uzun aday tercih edilir
	// (duz metin anmasi yerine gercek cagri satiri).
	reInline := regexp.MustCompile("`([^`]{3,200})`")
	best := ""
	for _, m := range reInline.FindAllStringSubmatch(body, -1) {
		cmd := strings.TrimSpace(m[1])
		if !looksLikeCommand(cmd) {
			continue
		}
		if strings.Contains(cmd, " ") {
			if len(cmd) > len(best) {
				best = cmd
			}
		} else if best == "" {
			best = cmd
		}
	}
	return best
}

func looksLikeCommand(s string) bool {
	known := []string{"go test", "go run", "go build", "go vet", "cargo",
		"simcraft-check", "simcraft-play", "simcraft-view",
		"./", "make", "cmake", "python", "pip", "npm", "npx"}
	s = strings.ToLower(s)
	for _, k := range known {
		if strings.HasPrefix(s, k) {
			return true
		}
	}
	return false
}

func getHeadSHA(repo string, prNum int) (string, error) {
	out, err := exec.Command("gh", "pr", "view",
		"--repo", fmt.Sprintf("%s/%s", owner, repo),
		"--json", "headRefOid",
		"--jq", ".headRefOid",
		fmt.Sprintf("%d", prNum)).Output()
	if err != nil {
		return "", fmt.Errorf("gh pr view: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	sha := strings.TrimSpace(string(out))
	if len(sha) < 7 {
		return "", fmt.Errorf("gecersiz SHA: %q", sha)
	}
	return sha, nil
}

func cloneRepo(repo, sha, dest string) error {
	url := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	c := exec.Command("git", "clone", "--depth=1", url, dest)
	c.Env = baseEnv()
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("clone: %w (%s)", err, tail(string(out), 200))
	}
	f := exec.Command("git", "-C", dest, "fetch", "--depth=1", "origin", sha)
	f.Env = baseEnv()
	if out, err := f.CombinedOutput(); err != nil {
		return fmt.Errorf("fetch sha: %w (%s)", err, tail(string(out), 200))
	}
	co := exec.Command("git", "-C", dest, "checkout", "--detach", sha)
	co.Env = baseEnv()
	if out, err := co.CombinedOutput(); err != nil {
		return fmt.Errorf("checkout: %w (%s)", err, tail(string(out), 200))
	}
	return nil
}

func revParse(dir string) (string, error) {
	c := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	c.Env = baseEnv()
	out, err := c.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func detectBuildStep(dir, command string) (bool, string) {
	if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
		if strings.HasPrefix(command, "cargo ") {
			return false, ""
		}
		return true, "cargo build --release"
	}
	return false, ""
}

// commandEnv: komutun klon icindeki derlenmis ikilileri (target/release,
// target/debug) ve klon kokunu gormesini saglar.
func commandEnv(dir string) []string {
	path := strings.Join([]string{
		dir,
		filepath.Join(dir, "target", "release"),
		filepath.Join(dir, "target", "debug"),
		"/usr/local/bin", "/usr/bin", "/bin", "/root/.cargo/bin",
	}, ":")
	return []string{
		"PATH=" + path,
		"HOME=/root",
		"GOCACHE=" + filepath.Join(dir, ".gocache"),
		"CARGO_TERM_COLOR=never",
		"GIT_TERMINAL_PROMPT=0",
		"RUST_BACKTRACE=0",
	}
}

func baseEnv() []string {
	return []string{
		"PATH=/usr/local/bin:/usr/bin:/bin:/root/.cargo/bin",
		"HOME=/root",
		"CARGO_TERM_COLOR=never",
		"GIT_TERMINAL_PROMPT=0",
		"RUST_BACKTRACE=0",
	}
}

func runCommand(dir, command string) (int, string, error) {
	c := exec.Command("sh", "-c", command)
	c.Dir = dir
	c.Env = commandEnv(dir)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = c.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(runTimeout):
		if c.Process != nil {
			_ = c.Process.Kill()
		}
		<-done
		return -2, string(out), fmt.Errorf("komut %s icinde bitmedi (timeout)", runTimeout)
	}
	output := string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), output, nil
		}
		return -1, output, err
	}
	return 0, output, nil
}

// cleanBetweenRuns: ayni satirin kosulari arasinda derleme ciktisi cache'ini
// siler; boylece her kosum sicak cache'ten faydalanmaz.
// COUNTRERUN_CLEAN_BETWEEN=1 ile acilir (Rust'ta 3x rebuild demektir).
func cleanBetweenRuns(dir string) {
	if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
		c := exec.Command("cargo", "clean", "-q")
		c.Dir = dir
		c.Env = baseEnv()
		_ = c.Run()
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		c := exec.Command("go", "clean", "-cache")
		c.Dir = dir
		c.Env = baseEnv()
		_ = c.Run()
	}
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func writeCSV(path, toolchain string, results []RunResult, runnable, private, nocommand, pass, fail, dnr int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# Toolchain: %s\n", strings.ReplaceAll(toolchain, "\n", " | "))
	fmt.Fprintf(f, "# Kosulabilir=%d private=%d komutsuz=%d PASS=%d FAIL=%d DOES-NOT-RESOLVE=%d\n",
		runnable, private, nocommand, pass, fail, dnr)
	fmt.Fprintf(f, "repo,pr,head_sha,command,access,runs,per_run_values,median_seconds,rc,result,artifact,reason_if_unresolved\n")
	for _, r := range results {
		fmt.Fprintf(f, "%s,%d,%s,%s,%s,%d,%s,%.3f,%d,%s,%s,%s\n",
			r.Repo, r.PR, r.HeadSHA, escapeCSV(r.Command),
			r.Access, r.Runs, perRun(r.PerRunValues), r.MedianSeconds,
			r.RC, r.Result, r.Artifact, escapeCSV(r.ReasonUnresolved))
	}
	return nil
}

func writeMD(path, toolchain string, results []RunResult, runnable, private, nocommand, pass, fail, dnr int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# Count/Rerun Raporu\n\n")
	fmt.Fprintf(f, "**Toolchain**\n\n```\n%s```\n\n", toolchain)
	fmt.Fprintf(f, "**Manset:** kosulabilir=%d private=%d komutsuz=%d | PASS=%d FAIL=%d DOES-NOT-RESOLVE=%d\n\n",
		runnable, private, nocommand, pass, fail, dnr)
	fmt.Fprintf(f, "| repo | pr | head_sha | command | access | runs | per_run_values | median_seconds | rc | result | artifact | reason_if_unresolved |\n")
	fmt.Fprintf(f, "|------|----|----------|---------|--------|------|---------------|---------------|-----|--------|----------|----------------------|\n")
	for _, r := range results {
		fmt.Fprintf(f, "| %s | %d | %s | %s | %s | %d | %s | %.3f | %d | %s | %s | %s |\n",
			r.Repo, r.PR, r.HeadSHA, escapeMD(r.Command),
			r.Access, r.Runs, perRun(r.PerRunValues), r.MedianSeconds,
			r.RC, r.Result, escapeMD(r.Artifact), escapeMD(r.ReasonUnresolved))
	}
	fmt.Fprintf(f, "\n## Receipt\n\n")
	for _, r := range results {
		fmt.Fprintf(f, "- `%s#%d` head_sha=`%s` clone_sha=`%s` sha_match=`%t` artifact=`%s` out_sha256=`%s`\n",
			r.Repo, r.PR, r.HeadSHA, r.CloneSHA, r.SHAVerified, r.Artifact, r.OutSHA)
	}
	return nil
}

func perRun(vals []float64) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%.3f", v)
	}
	return strings.Join(parts, " ")
}

func escapeCSV(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}

func escapeMD(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
