package daemon

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// DoctorServiceReplacement selects a verified loaded LaunchAgent or the pinned
// foreground/down entry point. The caller has already retained dispatch intent.
type DoctorServiceReplacement struct {
	DB         *store.DB
	Paths      config.Paths
	Foreground *SameBinaryDoctorReplacement
	Manager    *launchd.Manager
}

// NewDoctorServiceReplacement adds installed-service routing to the pinned binary.
// Construction performs no launchctl call or external process effect.
func NewDoctorServiceReplacement(db *store.DB, executable string, paths config.Paths, environment []string) (*DoctorServiceReplacement, error) {
	foreground, err := NewSameBinaryDoctorReplacement(executable, paths, environment)
	if err != nil {
		return nil, err
	}
	r := &DoctorServiceReplacement{DB: db, Paths: paths, Foreground: foreground}
	if runtime.GOOS == "darwin" {
		r.Manager = &launchd.Manager{}
	}
	return r, nil
}

// PreflightDoctorReplacement checks the selected route before repair mutation.
func (r *DoctorServiceReplacement) PreflightDoctorReplacement(ctx context.Context) error {
	if r.Foreground == nil {
		return errors.New("doctor pinned replacement unavailable")
	}
	digest, err := doctorExecutableDigest(r.Foreground.executable)
	if err != nil || digest != r.Foreground.digest {
		return errors.New("doctor replacement executable changed")
	}
	if r.Manager == nil {
		return nil
	}
	status, err := r.Manager.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Loaded {
		return nil
	}
	if !status.Installed {
		return errors.New("doctor loaded service has no inspectable installation")
	}
	if err = verifyDoctorLaunchAgent(status.PlistPath, r.Foreground.executable, r.Paths.Home()); err != nil {
		return err
	}
	return r.Manager.VerifyRecoveryIdentity(ctx, r.Foreground.executable, r.Paths.Home())
}

// StartDoctorReplacement carries the exact token through ordinary launchd
// startup without rewriting the installed plist or relying on a PID alone.
func (r *DoctorServiceReplacement) StartDoctorReplacement(ctx context.Context, intent store.DoctorRestart) error {
	if r.Foreground == nil {
		return errors.New("doctor pinned replacement unavailable")
	}
	if r.Manager == nil {
		return r.Foreground.StartDoctorReplacement(ctx, intent)
	}
	status, err := r.Manager.Status(ctx)
	if err != nil {
		return err
	}
	if !status.Loaded {
		return r.Foreground.StartDoctorReplacement(ctx, intent)
	}
	if !status.Installed {
		return errors.New("doctor loaded service has no inspectable installation")
	}
	if status.Running && status.PID != os.Getpid() {
		return errors.New("doctor launchd process does not match singleton owner")
	}
	if !status.Running && !strings.HasPrefix(intent.OldInstance, "down:") {
		return errors.New("doctor live launchd identity unavailable")
	}
	digest, err := doctorExecutableDigest(r.Foreground.executable)
	if err != nil || digest != r.Foreground.digest {
		return errors.New("doctor launchd executable changed")
	}
	if err = verifyDoctorLaunchAgent(status.PlistPath, r.Foreground.executable, r.Paths.Home()); err != nil {
		return err
	}
	if err = r.Manager.VerifyRecoveryIdentity(ctx, r.Foreground.executable, r.Paths.Home()); err != nil {
		return err
	}
	if _, err = readDoctorHandoff(ctx, r.DB, intent); err != nil {
		return err
	}
	if err = retainDoctorLaunchdRecovery(ctx, r.DB, r.Paths, intent); err != nil {
		return err
	}
	// kickstart may terminate this caller before its reply. The durable marker and
	// token precede it; absence of a reply never permits another dispatch.
	return r.Manager.Restart(ctx)
}

type doctorPlistNode struct {
	XMLName  xml.Name
	Text     string            `xml:",chardata"`
	Children []doctorPlistNode `xml:",any"`
}

func verifyDoctorLaunchAgent(path, executable, home string) error {
	//nolint:gosec // Manager-owned installed plist path, checked through the descriptor below.
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	before, err := os.Lstat(path)
	if err != nil || !ok || !info.Mode().IsRegular() || !before.Mode().IsRegular() || !os.SameFile(before, info) || info.Mode().Perm()&0077 != 0 || int(stat.Uid) != os.Geteuid() || info.Size() > 1<<20 {
		return errors.New("doctor installed plist unsafe")
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return err
	}
	var tree doctorPlistNode
	if err = xml.Unmarshal(data, &tree); err != nil {
		return errors.New("doctor installed plist invalid")
	}
	if tree.XMLName.Local != "plist" || len(tree.Children) != 1 {
		return errors.New("doctor installed plist root invalid")
	}
	fields, err := doctorPlistDictionary(tree.Children[0])
	if err != nil {
		return err
	}
	args := fields["ProgramArguments"].Children
	if (len(args) != 3 && len(args) != 5) || args[0].Text != executable || args[1].Text != "daemon" || args[2].Text != "run" {
		return errors.New("doctor installed service executable or arguments differ")
	}
	if len(args) == 5 && (args[3].Text != "--level" || !slices.Contains([]string{"debug", "info", "warn", "error"}, args[4].Text)) {
		return errors.New("doctor installed service arguments refused")
	}
	env, err := doctorPlistDictionary(fields["EnvironmentVariables"])
	if err != nil {
		return err
	}
	if env[config.EnvHome].Text != home {
		return errors.New("doctor installed service belongs to another factory home")
	}
	return nil
}
func doctorPlistDictionary(node doctorPlistNode) (map[string]doctorPlistNode, error) {
	out := map[string]doctorPlistNode{}
	if node.XMLName.Local != "dict" || len(node.Children)%2 != 0 {
		return nil, errors.New("doctor plist dictionary malformed")
	}
	for i := 0; i < len(node.Children); i += 2 {
		if node.Children[i].XMLName.Local != "key" {
			return nil, errors.New("doctor plist dictionary key malformed")
		}
		key := node.Children[i].Text
		if key == "" {
			return nil, errors.New("doctor plist key empty")
		}
		if _, exists := out[key]; exists {
			return nil, errors.New("doctor plist duplicate key")
		}
		out[key] = node.Children[i+1]
	}
	return out, nil
}

func retainDoctorLaunchdRecovery(ctx context.Context, db *store.DB, paths config.Paths, intent store.DoctorRestart) error {
	root, err := doctorPrivateRoot(paths.DoctorDispatchDir())
	if err != nil {
		return err
	}
	defer root.Close()
	previous, err := ReadDoctorLaunchdRecovery(ctx, db, paths)
	if err != nil {
		return err
	}
	if previous != nil {
		return errors.New("doctor launchd recovery already pending; observe without retry")
	}
	// Only an acknowledged old token can reach here when a pointer exists.
	if err = root.Remove("launchd.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := root.OpenFile("launchd.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	data, err := json.Marshal(store.DoctorRestart{RepairID: intent.RepairID, Token: intent.Token})
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	return syncDoctorDirectory(paths.DoctorDispatchDir())
}

// ReadDoctorLaunchdRecovery loads a retained exact token before startup recovery.
// Acknowledged pointers remain historical and do not request a second restart.
func ReadDoctorLaunchdRecovery(ctx context.Context, db *store.DB, paths config.Paths) (*store.DoctorRestart, error) {
	root, err := doctorPrivateRoot(paths.DoctorDispatchDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	before, err := root.Lstat("launchd.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || int(stat.Uid) != os.Geteuid() || before.Size() > 65536 {
		return nil, errors.New("doctor launchd token unsafe")
	}
	file, err := root.Open("launchd.json")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) {
		return nil, errors.New("doctor launchd token changed")
	}
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(data) > 65536 {
		return nil, errors.New("doctor launchd token exceeds bound")
	}
	var intent store.DoctorRestart
	if err = submission.DecodeJSON(data, &intent); err != nil {
		return nil, err
	}
	saved, err := db.ReadDoctorRestart(ctx, intent.RepairID)
	if err != nil {
		return nil, err
	}
	if saved.Token != intent.Token {
		return nil, errors.New("doctor launchd token identity differs")
	}
	if saved.State == "acknowledged" {
		return nil, nil
	}
	if _, err = readDoctorHandoff(ctx, db, intent); err != nil {
		return nil, err
	}
	return &saved, nil
}
