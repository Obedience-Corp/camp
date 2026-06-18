// Package promote holds the shared festival-creation seam used by the intent
// and workitem promote flows.
package promote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fest"
)

type Promotable interface {
	Title() string
	GoalText() string
	PrimaryDocPath() string
}

type FestivalResult struct {
	Name      string
	Dir       string
	Dest      string
	Created   bool
	DocCopied bool
	NotFound  bool
	CLIError  string
}

type festCreateOutput struct {
	OK       bool              `json:"ok"`
	Festival map[string]string `json:"festival"`
}

func FindAndCreateFestival(ctx context.Context, campaignRoot, name, goal string) (FestivalResult, error) {
	festPath, err := fest.FindFestCLI()
	if err != nil {
		return FestivalResult{NotFound: true}, nil
	}

	args := []string{"create", "festival", "--type", "standard", "--name", name, "--json"}
	if goal != "" {
		args = append(args, "--goal", goal)
	}

	cmd := exec.CommandContext(ctx, festPath, args...)
	cmd.Dir = campaignRoot
	output, err := cmd.Output()
	if err != nil {
		return FestivalResult{Name: name, CLIError: extractFestStderr(err)}, nil
	}

	var festOut festCreateOutput
	if err := json.Unmarshal(output, &festOut); err != nil || !festOut.OK {
		return FestivalResult{Name: name}, nil
	}

	dir := festOut.Festival["directory"]
	dest := festOut.Festival["dest"]
	if dir == "" {
		dir = name
	}
	if dest == "" {
		dest = "planning"
	}

	return FestivalResult{Name: name, Dir: dir, Dest: dest, Created: true}, nil
}

func extractFestStderr(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return strings.TrimSpace(string(exitErr.Stderr))
	}
	return ""
}

func CopyIntoFestivalIngest(campaignRoot, dest, festivalDir, srcDoc string) bool {
	if srcDoc == "" {
		return false
	}

	ingestDir := filepath.Join(campaignRoot, "festivals", dest, festivalDir, "001_INGEST", "input_specs")
	if _, err := os.Stat(ingestDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr,
			"Notice: source doc not copied to festival ingest directory because %s does not exist.\n"+
				"File to ingest: %s\n",
			ingestDir, srcDoc)
		return false
	} else if err != nil {
		return false
	}

	destPath := filepath.Join(ingestDir, filepath.Base(srcDoc))
	return copyFile(srcDoc, destPath) == nil
}

func CopyTree(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := filepath.Base(path)
		if base == ".workitem" || strings.HasPrefix(base, ".workflow") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return camperrors.Wrap(err, "opening source file")
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return camperrors.Wrap(err, "creating destination file")
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	if err != nil {
		return camperrors.Wrap(err, "copying file contents")
	}
	return nil
}

type PromotionRecord struct {
	PromotedTo string
	PromotedAt time.Time
}

func RecordPromotion(target string, save func(PromotionRecord) error) error {
	return save(PromotionRecord{PromotedTo: target, PromotedAt: time.Now().UTC()})
}

func ExtractFirstParagraph(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	paragraphs := strings.Split(content, "\n\n")
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		if strings.HasPrefix(p, "#") {
			lines := strings.SplitN(p, "\n", 2)
			if len(lines) > 1 {
				return strings.TrimSpace(lines[1])
			}
			continue
		}

		return p
	}

	return ""
}
