package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/samuelbutton/copernicus/compatibility"
	"github.com/samuelbutton/copernicus/internal/request"
)

const MaxImportFiles = 10000

// Reader reads public folders only. It never opens an engine database.
type Reader struct {
	Path string
	root *os.Root
}

func OpenReader(path string) (*Reader, error) {
	if path == "" {
		return nil, errors.New("exchange directory is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	return &Reader{Path: absolute, root: root}, nil
}
func (r *Reader) Close() error { return r.root.Close() }

type ResultFile struct {
	Path    string
	SHA256  string
	Content []byte
	Result  compatibility.Result
	Job     compatibility.Job
}
type Observation struct {
	Path       string
	SHA256     string
	Event      *compatibility.Event
	Result     *ResultFile
	Identities map[string]string
}

var publicPath = regexp.MustCompile(`^(jobs|events|results|bags)/[a-z0-9][a-z0-9_-]{0,127}\.(json|jsonl)$`)

func (r *Reader) directory(folder string) (*os.Root, error) {
	info, err := r.root.Lstat(folder)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("public folder must be a directory, not a symbolic link")
	}
	return r.root.OpenRoot(folder)
}

// Candidates rescans names rather than using a filename cursor. Late files can
// sort before an earlier event; persisted event identities provide deduplication.
func (r *Reader) Candidates(folder string) ([]string, error) {
	if folder != "results" && folder != "events" {
		return nil, errors.New("unsupported scan folder")
	}
	scope, err := r.directory(folder)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer scope.Close()
	dir, err := scope.Open(".")
	if err != nil {
		return nil, err
	}
	entries, err := dir.ReadDir(MaxImportFiles + 1)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err = errors.Join(err, dir.Close()); err != nil {
		return nil, err
	}
	if len(entries) > MaxImportFiles {
		return nil, errors.New("public folder exceeds scan limit")
	}
	paths := []string{}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			paths = append(paths, folder+"/"+entry.Name())
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (r *Reader) read(ctx context.Context, path, kind string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	suffix := ".json"
	limit := int64(compatibility.MaxJobBytes)
	if kind == "bag" {
		suffix = ".jsonl"
		limit = compatibility.MaxBagBytes
	}
	if !publicPath.MatchString(path) || !strings.HasPrefix(path, kind+"s/") || !strings.HasSuffix(path, suffix) {
		return nil, errors.New("invalid public file path")
	}
	scope, err := r.directory(kind + "s")
	if err != nil {
		return nil, err
	}
	defer scope.Close()
	f, err := scope.OpenFile(strings.TrimPrefix(path, kind+"s/"), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.Join(errors.New("expected bounded regular file"), err, f.Close())
	}
	data, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	if err := errors.Join(readErr, f.Close()); err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("public file exceeds size limit")
	}
	return data, ctx.Err()
}

func (r *Reader) referenced(ctx context.Context, ref compatibility.Reference, kind string, identities map[string]string) ([]byte, error) {
	data, err := r.read(ctx, ref.Path, kind)
	if err != nil {
		return nil, err
	}
	if request.Hash(data) != ref.SHA256 {
		return nil, errors.New("referenced file hash mismatch")
	}
	identities["path/"+ref.Path] = ref.SHA256
	return data, nil
}

// Observe validates the complete finite event/result/job/bag reference chain.
// Only a fully validated observation may enter the local index transaction.
func (r *Reader) Observe(ctx context.Context, path string) (Observation, error) {
	out := Observation{Path: path, Identities: map[string]string{}}
	switch {
	case strings.HasPrefix(path, "results/"):
		result, err := r.result(ctx, path, out.Identities)
		if err != nil {
			return Observation{}, err
		}
		out.Result = &result
		out.SHA256 = result.SHA256
	case strings.HasPrefix(path, "events/"):
		data, err := r.read(ctx, path, "event")
		if err != nil {
			return Observation{}, err
		}
		event, err := compatibility.ParseEvent(data)
		if err != nil {
			return Observation{}, err
		}
		out.Event = &event
		out.SHA256 = request.Hash(data)
		out.Identities["event/"+event.EventID] = out.SHA256
		out.Identities["path/"+path] = out.SHA256
		if event.Result != nil {
			result, err := r.result(ctx, event.Result.Path, out.Identities)
			if err != nil {
				return Observation{}, err
			}
			if result.SHA256 != event.Result.SHA256 {
				return Observation{}, errors.New("event result hash mismatch")
			}
			if err := compatibility.CheckEventResult(event, result.Result); err != nil {
				return Observation{}, err
			}
			out.Result = &result
		}
	default:
		return Observation{}, errors.New("select an event or result path")
	}
	return out, nil
}

func (r *Reader) result(ctx context.Context, path string, identities map[string]string) (ResultFile, error) {
	var out ResultFile
	data, err := r.read(ctx, path, "result")
	if err != nil {
		return out, err
	}
	if err := compatibility.ValidateDocument(data, "result", &out.Result); err != nil {
		return out, err
	}
	out.Path, out.SHA256, out.Content = path, request.Hash(data), data
	jobBytes, err := r.referenced(ctx, out.Result.Job, "job", identities)
	if err != nil {
		return out, err
	}
	out.Job, err = compatibility.ParseJob(jobBytes)
	if err != nil {
		return out, err
	}
	identities["job/"+out.Job.JobID] = out.Result.Job.SHA256
	if out.Job.JobKind == "run" {
		identities["execution/"+out.Job.ExecutionID] = out.Result.Job.SHA256
	}
	var bag compatibility.BagHeader
	// Analysis jobs refer to a bag even when a malformed result omits it.
	if out.Result.Bag != nil {
		bagBytes, err := r.referenced(ctx, *out.Result.Bag, "bag", identities)
		if err != nil {
			return out, err
		}
		bag, err = compatibility.ParseBag(ctx, bagBytes)
		if err != nil {
			return out, err
		}
		identities["bag/"+bag.ExecutionID] = out.Result.Bag.SHA256
	}
	if err := compatibility.CheckResult(out.Result, out.Job, bag); err != nil {
		return out, fmt.Errorf("result relationships: %w", err)
	}
	identities[out.Result.Identity()] = out.SHA256
	identities["outcome/"+out.Result.JobID] = out.SHA256
	identities["path/"+path] = out.SHA256
	return out, nil
}

// Tick returns one cited record after checking the recording's exact bytes.
func (r *Reader) Tick(ctx context.Context, ref compatibility.Reference, tick int) (json.RawMessage, error) {
	data, err := r.referenced(ctx, ref, "bag", map[string]string{})
	if err != nil {
		return nil, err
	}
	header, err := compatibility.ParseBag(ctx, data)
	if err != nil {
		return nil, err
	}
	if tick < 0 || tick >= header.RecordCount {
		return nil, errors.New("tick outside recording")
	}
	lines := bytes.Split(data, []byte("\n"))
	return json.RawMessage(lines[tick+1]), nil
}
