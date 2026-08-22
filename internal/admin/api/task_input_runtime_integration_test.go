package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime"
	"github.com/oopslink/agent-center/internal/agentruntime/reporepo"
	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/files"
	filesservice "github.com/oopslink/agent-center/internal/files/service"
	filessql "github.com/oopslink/agent-center/internal/files/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// handlerToolCaller deliberately has no canned responses: every get_task,
// list_files, start_task and file download crosses the real admin HTTP handlers.
type handlerToolCaller struct {
	base, token string
}

type instantCloneMaterializer struct{}

type sequenceIDGen struct {
	ids []string
	i   int
}

func (g *sequenceIDGen) NewULID() string {
	if g.i >= len(g.ids) {
		panic("sequenceIDGen exhausted")
	}
	id := g.ids[g.i]
	g.i++
	return id
}

func (g *sequenceIDGen) NewEntityID(prefix string) string { return prefix + "-00000000" }

func (f *writeToolsFixture) attachAgentFilesSvcWithIDGen(t *testing.T, gen *sequenceIDGen) {
	t.Helper()
	store, err := blobstore.NewLocalDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.deps.FilesSvc = filesservice.New(filesservice.Deps{
		DB:         f.db,
		Sessions:   filessql.NewFileTransferSessionRepo(f.db),
		References: filessql.NewFileReferenceRepo(f.db),
		Resolver:   files.NewLocalResolver(""),
		BlobStore:  store,
		IDGen:      gen,
		Clock:      clock.SystemClock{},
	}).SetGCRepo(filessql.NewBlobMetadataRepo(f.db))
}

func (instantCloneMaterializer) PrepareClone(_ context.Context, target reporepo.RepoTarget, req reporepo.CloneRequest) (reporepo.PreparedClone, error) {
	if err := os.MkdirAll(req.WorkspacePath, 0o700); err != nil {
		return reporepo.PreparedClone{}, err
	}
	return reporepo.PreparedClone{ExecutorID: req.ExecutorID, RepoKey: target.RepoID, WorkspacePath: req.WorkspacePath, Branch: req.BranchName, BaseRef: "0123456789abcdef0123456789abcdef01234567"}, nil
}

func (c *handlerToolCaller) CallAgentTool(ctx context.Context, tool string, body any, out *json.RawMessage) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/admin/agent-tools/"+tool, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: status %d: %s", tool, resp.StatusCode, raw)
	}
	// The fixture's legacy URL-only repo reference has no branch discovery. Supply the
	// same resolved base_ref production repo discovery adds; all task/file fields remain
	// the real get_task handler response.
	if tool == "get_task" {
		var task map[string]any
		if err := json.Unmarshal(raw, &task); err != nil {
			return err
		}
		task["base_ref"] = "main"
		raw, _ = json.Marshal(task)
	}
	if out != nil {
		*out = append((*out)[:0], raw...)
	}
	return nil
}

func (c *handlerToolCaller) DownloadFile(ctx context.Context, agentID, uri, dest string) error {
	ulid := strings.TrimPrefix(uri, "ac://files/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/admin/files/"+ulid+"?agent_id="+agentID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download status %d: %s", resp.StatusCode, raw)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, b, 0o600)
}

func uploadTaskInput(t *testing.T, base, token, taskID, name, mime string, content []byte) string {
	t.Helper()
	status, body := postBearer(t, base, "/admin/agent-tools/upload_file", token, map[string]any{
		"agent_id": atAgent1, "filename": name, "content_type": mime, "size": len(content), "scope": "task", "scope_id": taskID,
	})
	if status != http.StatusOK {
		t.Fatalf("upload_file status=%d body=%v", status, body)
	}
	transferID, _ := body["transfer_id"].(string)
	fileURI, _ := body["file_uri"].(string)
	if status, body := putBearer(t, base, "/admin/files/transfer/"+transferID+"?agent_id="+atAgent1, token, content); status != http.StatusOK {
		t.Fatalf("put status=%d body=%v", status, body)
	}
	sum := sha256.Sum256(content)
	status, body = postBearer(t, base, "/admin/files/transfer/"+transferID+"/complete", token, map[string]any{
		"agent_id": atAgent1, "sha256": hex.EncodeToString(sum[:]), "size": len(content), "scope": "task", "scope_id": taskID, "filename": name,
	})
	if status != http.StatusOK {
		t.Fatalf("complete status=%d body=%v", status, body)
	}
	return fileURI
}

func encodedImage(t *testing.T, kind string, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x + 1), G: uint8(y + 2), B: 90, A: 255})
		}
	}
	var b bytes.Buffer
	var err error
	if kind == "png" {
		err = png.Encode(&b, img)
	} else {
		err = jpeg.Encode(&b, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestTaskInputPlan569_RealAdminHandlersEndToEnd(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	canonical := []struct {
		uri, ulid, transfer, reference, name, mime, kind string
		width, height                                    int
	}{
		{"ac://files/01M0HRMZDX20FF5KQT4SBANGC1", "01M0HRMZDX20FF5KQT4SBANGC1", "01M0HRMZDX20FF5KQT4SBANGT1", "01M0HRMZDX20FF5KQT4SBANGR1", "plan-569-t1456.png", "image/png", "png", 1672, 941},
		{"ac://files/01M0HRMZEV7XS8A3MNGG64ZZW1", "01M0HRMZEV7XS8A3MNGG64ZZW1", "01M0HRMZEV7XS8A3MNGG64ZZT1", "01M0HRMZEV7XS8A3MNGG64ZZR1", "plan-569-t1457.png", "image/png", "png", 1672, 941},
		{"ac://files/01M0HRMZFD0Q7NA194RQNYYVBW", "01M0HRMZFD0Q7NA194RQNYYVBW", "01M0HRMZFD0Q7NA194RQNYYVBT", "01M0HRMZFD0Q7NA194RQNYYVBR", "plan-569-t1458.png", "image/png", "png", 1684, 934},
	}
	var ids []string
	for _, c := range canonical {
		ids = append(ids, c.ulid, c.transfer, c.reference)
	}
	f.attachAgentFilesSvcWithIDGen(t, &sequenceIDGen{ids: ids})
	srv := f.filesServer(t)
	taskID := f.seedOpenAssignedPoolTask(t)
	task, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pmSvc.AddCodeRepoReference(context.Background(), pmservice.AddCodeRepoReferenceCommand{
		ProjectID: task.ProjectID(), URL: "https://example.invalid/task-input.git", IsPrimary: true, Actor: pm.IdentityRef("user:owner"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(context.Background(), `UPDATE code_repos SET default_branch='main' WHERE url='https://example.invalid/task-input.git'`); err != nil {
		t.Fatal(err)
	}

	want := map[string]struct {
		bytes         []byte
		name          string
		width, height int
	}{}
	for _, tc := range canonical {
		b := encodedImage(t, tc.kind, tc.width, tc.height)
		uri := uploadTaskInput(t, srv.URL, "acat_w1", taskID, tc.name, tc.mime, b)
		if uri != tc.uri {
			t.Fatalf("upload returned URI %q, want canonical %s", uri, tc.uri)
		}
		want[uri] = struct {
			bytes         []byte
			name          string
			width, height int
		}{b, tc.name, tc.width, tc.height}
	}

	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("true unavailable: %v", err)
	}
	home := filepath.Join(t.TempDir(), "agent-home")
	caller := &handlerToolCaller{base: srv.URL, token: "acat_w1"}
	rt := agentruntime.NewLocalRuntime(agentruntime.LocalRuntimeConfig{
		AgentID: atAgent1, WorkerID: atWorker1, AgentHomeBase: filepath.Dir(home), BinaryPath: trueBin,
		ToolCaller: func() agentruntime.ToolCaller { return caller }, Log: t.Logf,
		CloneMaterializer: instantCloneMaterializer{}, ClonePrepareTimeout: time.Second,
	}, &agentruntime.SessionState{})
	ee, err := rt.BuildExecutorEngine(home, agentruntime.ExecutorConfig{AgentID: atAgent1, MaxConcurrentTasks: 1, DefaultExecutorModel: "m"})
	if err != nil {
		t.Fatal(err)
	}
	rt.AttachExecutor(ee)
	res, err := rt.SpawnExecutor(context.Background(), agentruntime.SpawnRequest{TaskID: taskID})
	if err != nil {
		t.Fatalf("SpawnExecutor res=%+v err=%v", res, err)
	}
	// Clone preparation is intentionally off the control path; its automatic redrive
	// performs the actual admission/materialization/fork.
	var manifestPath string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(home, "executors", "*", "workspace", "task-input", "v1", "manifest.json"))
		if len(matches) == 1 {
			manifestPath = matches[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if manifestPath == "" {
		t.Fatal("automatic clone redrive did not publish task-input/v1 manifest")
	}
	workspace := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))
	var manifest struct {
		Files []struct {
			URI, Name, SHA256, Path string
			Size                    int64 `json:"size"`
			Width, Height           int
		} `json:"files"`
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 3 {
		t.Fatalf("manifest files=%d want 3", len(manifest.Files))
	}
	for _, got := range manifest.Files {
		w, ok := want[got.URI]
		if !ok {
			t.Fatalf("unexpected uri %q", got.URI)
		}
		sum := sha256.Sum256(w.bytes)
		if got.Name != w.name || got.SHA256 != hex.EncodeToString(sum[:]) || got.Size != int64(len(w.bytes)) || got.Width != w.width || got.Height != w.height {
			t.Fatalf("manifest mismatch uri=%s got=%+v", got.URI, got)
		}
		local, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(got.Path)))
		if err != nil || !bytes.Equal(local, w.bytes) {
			t.Fatalf("materialized bytes mismatch uri=%s err=%v", got.URI, err)
		}
	}
}
