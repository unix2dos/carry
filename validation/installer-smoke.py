#!/usr/bin/env python3
"""Exercise installation in disposable homes with local download/npm fixtures."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile

REPO = Path(__file__).resolve().parents[1]
VERSION = 'v0.1.0-alpha.2'
ASSET = f'carry-{VERSION}-darwin-arm64.tar.gz'


def executable(path, body):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(body)
    path.chmod(0o755)


def main():
    with tempfile.TemporaryDirectory(prefix='carry-installer-') as temporary:
        root = Path(temporary)
        fixtures = root / 'fixtures'
        fixtures.mkdir()
        bundle = root / 'bundle'
        executable(bundle / 'bin/carry', f'#!/bin/sh\nprintf "carry {VERSION}\\n"\n')
        (bundle / 'VERSION').write_text(VERSION + '\n')
        (bundle / 'skills/carry').mkdir(parents=True)
        shutil.copy2(REPO / 'skills/carry/SKILL.md', bundle / 'skills/carry/SKILL.md')
        (bundle / 'tools').mkdir()
        (bundle / 'tools/package.json').write_text('{}')
        with tarfile.open(fixtures / ASSET, 'w:gz') as archive:
            archive.add(bundle, arcname='carry')
        checksum = hashlib.sha256((fixtures / ASSET).read_bytes()).hexdigest()
        (fixtures / (ASSET + '.sha256')).write_text(f'{checksum}  {ASSET}\n')
        fake_bin = root / 'fake-bin'
        executable(fake_bin / 'uname', '#!/bin/sh\ncase "$1" in -s) echo Darwin;; -m) echo arm64;; esac\n')
        (fake_bin / 'node').symlink_to(shutil.which('node'))
        executable(fake_bin / 'curl', f'#!{sys.executable}\n' + '''import os, pathlib, shutil, sys
url = next(x for x in sys.argv if x.startswith('https://'))
shutil.copyfile(pathlib.Path(os.environ['CARRY_TEST_FIXTURES']) / url.rsplit('/', 1)[1], sys.argv[sys.argv.index('-o') + 1])
''')
        executable(fake_bin / 'npm', f'#!{sys.executable}\n' + '''import os, pathlib, sys
if os.environ.get('CARRY_TEST_NPM_FAIL'):
    sys.exit(1)
assert pathlib.Path('package.json').is_file(), 'npm must run inside the dependency directory'
prefix = pathlib.Path.cwd() / 'node_modules/.bin'
prefix.mkdir(parents=True)
for name in ['railway', 'neon', 'vercel']:
    path = prefix / name
    path.write_text('#!/bin/sh\\necho fixture\\n')
    path.chmod(0o755)
''')

        def install(test_home, shell='zsh', extra=None, succeeds=True):
            test_home.mkdir(parents=True, exist_ok=True)
            env = {**os.environ, 'HOME': str(test_home), 'SHELL': f'/bin/{shell}',
                   'PATH': str(fake_bin) + os.pathsep + '/usr/bin:/bin:/usr/sbin:/sbin',
                   'CARRY_TEST_FIXTURES': str(fixtures)}
            env.pop('ZDOTDIR', None)
            env.update(extra or {})
            result = subprocess.run(['sh', str(REPO / 'scripts/install.sh')], env=env,
                                    text=True, capture_output=True, timeout=30)
            assert (result.returncode == 0) == succeeds, result.stdout + result.stderr
            return env

        for shell, profiles in [('zsh', ['.zshrc', '.zprofile']), ('bash', ['.bashrc', '.profile'])]:
            test_home = root / (shell + ' home with spaces')
            test_home.mkdir()
            for name in profiles:
                (test_home / name).write_text('# existing user configuration\n')
            env = install(test_home, shell)
            install(test_home, shell)
            binary = test_home / '.local/bin/carry'
            assert binary.is_symlink()
            for name in ['.agents/skills/carry', '.claude/skills/carry']:
                link = test_home / name
                assert link.is_symlink() and (link / 'SKILL.md').is_file()
            for name in profiles:
                content = (test_home / name).read_text()
                assert content.startswith('# existing user configuration\n')
                assert content.count('# carry') == 1, 'PATH entry duplicated'
            command = '. "$HOME/' + profiles[0] + '"; command -v carry; carry --version'
            output = subprocess.check_output(['sh', '-c', command], env=env, text=True)
            assert output.splitlines() == [str(binary), 'carry ' + VERSION]
            assert not (test_home / '.carry').exists(), 'Installer touched deployment state'

        # Existing Ship users retain their data and get Carry without duplicate Skills.
        test_home = root / 'ship-upgrade'
        legacy = test_home / '.local/share/ship'
        executable(legacy / 'bin/ship', '#!/bin/sh\necho old-ship\n')
        (legacy / 'skills/ship').mkdir(parents=True)
        (legacy / 'skills/ship/SKILL.md').write_text('legacy skill')
        (test_home / '.ship').mkdir()
        (test_home / '.ship/keep.json').write_text('existing private state')
        for name, target in [('.local/bin/ship', legacy / 'bin/ship'),
                             ('.agents/skills/ship', legacy / 'skills/ship'),
                             ('.claude/skills/ship', legacy / 'skills/ship')]:
            link = test_home / name
            link.parent.mkdir(parents=True, exist_ok=True)
            link.symlink_to(target)
        env = install(test_home)
        assert (test_home / '.local/bin/ship').resolve() == (test_home / '.local/bin/carry').resolve()
        assert not (test_home / '.agents/skills/ship').is_symlink()
        assert not (test_home / '.claude/skills/ship').is_symlink()
        assert (legacy / 'skills/ship/SKILL.md').read_text() == 'legacy skill'
        assert (test_home / '.ship/keep.json').read_text() == 'existing private state'
        assert not (test_home / '.carry').exists()
        install(test_home)

        # Respect custom zsh startup paths and an existing bash login profile.
        test_home = root / 'custom-shell'
        dotdir = test_home / 'zsh-config'
        install(test_home, extra={'ZDOTDIR': str(dotdir)})
        assert (dotdir / '.zshrc').is_file() and not (test_home / '.zshrc').exists()
        test_home = root / 'bash-existing'
        test_home.mkdir()
        (test_home / '.bash_profile').write_text('# keep\n')
        install(test_home, 'bash')
        assert '# carry' in (test_home / '.bash_profile').read_text()
        assert not (test_home / '.profile').exists()

        # A collision must fail before installing or overwriting the user's file.
        for name in ['.local/bin/carry', '.agents/skills/carry', '.claude/skills/carry']:
            test_home = root / ('conflict-' + name.split('/')[0])
            target = test_home / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text('owned by another tool')
            install(test_home, succeeds=False)
            assert target.read_text() == 'owned by another tool'
            assert not (test_home / '.local/share/carry').exists()

        for failure in ['npm', 'checksum']:
            test_home = root / ('failure-' + failure)
            if failure == 'checksum':
                (fixtures / ASSET).write_bytes(b'corrupted download')
            install(test_home, extra={'CARRY_TEST_NPM_FAIL': '1'} if failure == 'npm' else None,
                    succeeds=False)
            assert not (test_home / '.local/share/carry').exists()
            assert not (test_home / '.zshrc').exists()
            assert not (test_home / '.agents/skills/carry').exists()
    print('PASS PATH, Skill links, repeated install, shell profiles, conflicts and failed downloads; no network/cloud calls')


if __name__ == '__main__':
    main()
