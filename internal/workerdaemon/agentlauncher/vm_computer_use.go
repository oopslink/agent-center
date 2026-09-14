package agentlauncher

import (
	"encoding/json"
	"path/filepath"
)

func tartGuestCodexSQLiteHome(agentID string) string {
	return filepath.Join("/Users/admin/agent-center/state", agentID, "codex-sqlite")
}

// The GUI service launches its own Codex app-server for authentication. It needs
// the same login/proxy settings as the agent, and SQLite must live on the guest
// disk: VirtioFS shares do not support the locking required by Codex app-server.
func tartGuestComputerUseSetup(plan tartGuestMountPlan, agentHome, agentID string, env []string) string {
	serviceEnv := map[string]string{
		"PATH":              envValue(defaultGuestEnv(), "PATH"),
		"CODEX_HOME":        filepath.Join(agentHome, "codex-home"),
		"CODEX_SQLITE_HOME": tartGuestCodexSQLiteHome(agentID),
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
		if value := envValue(env, key); value != "" {
			serviceEnv[key] = value
		}
	}
	payload, _ := json.Marshal(map[string]any{"source": plan.codexHome, "env": serviceEnv})
	return "/usr/bin/python3 -c " + shellQuote(tartGuestComputerUseSetupScript) + " " + shellQuote(string(payload))
}

const tartGuestComputerUseSetupScript = `
import hashlib, json, os, pathlib, plistlib, shutil, subprocess, sys, tempfile, urllib.parse
cfg = json.loads(sys.argv[1])
pathlib.Path(cfg['env']['CODEX_SQLITE_HOME']).mkdir(parents=True, exist_ok=True)
# GUI apps use SystemConfiguration proxies, not HTTP_PROXY from a shell.
# Preserve the previous settings so turning the agent option off restores them.
proxy_state = pathlib.Path(cfg['env']['CODEX_SQLITE_HOME']).parent / 'system-proxy-before.json'
proxy = cfg['env'].get('HTTP_PROXY', '')
def network(*args):
    return subprocess.check_output(['/usr/sbin/networksetup', *args], text=True).strip()
def configure(*args):
    subprocess.run(['/usr/bin/sudo', '-n', '/usr/sbin/networksetup', *args], check=True)
if proxy or proxy_state.exists():
    if not proxy_state.exists():
        before = {}
        for service in network('-listallnetworkservices').splitlines()[1:]:
            if service and not service.startswith('*'):
                entry = {'bypass': network('-getproxybypassdomains', service).splitlines()}
                if entry['bypass'] and entry['bypass'][0].startswith("There aren't any"):
                    entry['bypass'] = []
                for kind in ['webproxy', 'securewebproxy']:
                    entry[kind] = dict(line.split(': ', 1) for line in network('-get'+kind, service).splitlines() if ': ' in line)
                before[service] = entry
        proxy_state.write_text(json.dumps(before))
    before = json.loads(proxy_state.read_text())
    for service, entry in before.items():
        for kind in ['webproxy', 'securewebproxy']:
            if proxy:
                url = urllib.parse.urlparse(proxy)
                configure('-set'+kind, service, url.hostname, str(url.port or 80))
                configure('-set'+kind+'state', service, 'on')
            else:
                prior = entry[kind]
                if prior.get('Enabled') == 'Yes':
                    configure('-set'+kind, service, prior['Server'], prior['Port'])
                configure('-set'+kind+'state', service, 'on' if prior.get('Enabled') == 'Yes' else 'off')
        bypass = ['localhost', '127.0.0.1', '192.168.*', '*.local', '169.254/16'] if proxy else entry['bypass']
        configure('-setproxybypassdomains', service, *(bypass or ['Empty']))
    if not proxy:
        proxy_state.unlink()

if not cfg['source']:
    sys.exit(0)
source = pathlib.Path(cfg['source']) / 'computer-use' / 'Codex Computer Use.app'
if not (source / 'Contents/MacOS/SkyComputerUseService').is_file():
    sys.exit(0)
root = pathlib.Path('/Users/admin/agent-center/computer-use')
root.mkdir(parents=True, exist_ok=True)
target = root / 'Codex Computer Use.app'
exe = target / 'Contents/MacOS/SkyComputerUseService'
def fingerprint(app):
    h = hashlib.sha256()
    try:
        for rel in ['Contents/Info.plist', 'Contents/MacOS/SkyComputerUseService']:
            with (app / rel).open('rb') as f:
                for chunk in iter(lambda: f.read(1024 * 1024), b''):
                    h.update(chunk)
        return h.digest()
    except FileNotFoundError:
        return None
changed_app = fingerprint(source) != fingerprint(target)
stage_root = None
if changed_app:
    stage_root = pathlib.Path(tempfile.mkdtemp(prefix='.computer-use-stage-', dir=root))
    staged = stage_root / target.name
    try:
        subprocess.run(['/usr/bin/ditto', str(source), str(staged)], check=True)
        subprocess.run(['/usr/bin/codesign', '--verify', '--deep', '--strict', str(staged)], check=True)
    except BaseException:
        shutil.rmtree(stage_root)
        raise
agents = pathlib.Path('/Users/admin/Library/LaunchAgents')
agents.mkdir(parents=True, exist_ok=True)
label = 'com.agent-center.sandbox-computeruse-app'
plist = agents / (label + '.plist')
log = pathlib.Path('/Users/admin/agent-center/run/computeruse-app.launchd.log')
log.parent.mkdir(parents=True, exist_ok=True)
data = plistlib.dumps({'Label': label, 'ProgramArguments': [str(exe)],
    'RunAtLoad': True, 'KeepAlive': True, 'EnvironmentVariables': cfg['env'],
    'StandardOutPath': str(log), 'StandardErrorPath': str(log)}, sort_keys=True)
domain = 'gui/' + str(os.getuid())
old_jobs = [p for p in agents.glob('com.agent-center.*computeruse-app.plist') if p != plist]
changed_job = not plist.exists() or plist.read_bytes() != data
if changed_app or changed_job or old_jobs:
    for p in old_jobs + [plist]:
        subprocess.run(['/bin/launchctl', 'bootout', domain, str(p)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    subprocess.run(['/usr/bin/pkill', '-f', str(exe)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    if changed_app:
        previous = root / 'Codex Computer Use.previous.app'
        if previous.exists():
            shutil.rmtree(previous)
        had_target = target.exists()
        if had_target:
            target.rename(previous)
        try:
            staged.rename(target)
        except BaseException:
            if had_target:
                previous.rename(target)
            raise
        finally:
            shutil.rmtree(stage_root)
    for p in old_jobs:
        p.rename(p.with_suffix('.plist.migrated'))
    temp = plist.with_suffix('.plist.tmp')
    temp.write_bytes(data)
    os.replace(temp, plist)
if subprocess.run(['/bin/launchctl', 'print', domain + '/' + label], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
    subprocess.run(['/bin/launchctl', 'bootstrap', domain, str(plist)], check=True)
`
