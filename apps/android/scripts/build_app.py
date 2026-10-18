#!/usr/bin/env python3
"""Build the Cully phone client without Gradle or Android Studio."""
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import zipfile
import hashlib

ROOT = Path(__file__).resolve().parents[1]
BUILD = ROOT / 'build/android'
JAVA = None


def run(*args):
    env = os.environ.copy()
    env['JAVA_HOME'] = str(JAVA)
    result = subprocess.run([str(x) for x in args], cwd=ROOT, env=env,
                            capture_output=True, text=True, timeout=180)
    if result.returncode:
        raise RuntimeError(result.stderr or result.stdout)
    return result.stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--compile-only', action='store_true')
    parser.add_argument('--engine-home', type=Path, required=True, help='Private local model/runtime workspace (not committed)')
    parser.add_argument('--mcp-sdk', type=Path, required=True, help='mcp-mobile target directory after mvn verify dependency:copy-dependencies -DincludeScope=runtime')
    parser.add_argument('--java-home', type=Path, default=os.environ.get('JAVA_HOME'))
    parser.add_argument('--android-sdk', type=Path, default=os.environ.get('ANDROID_HOME'))
    args = parser.parse_args()
    global JAVA
    if not args.java_home:
        raise RuntimeError('Set JAVA_HOME to a JDK 17 installation, or pass --java-home.')
    JAVA = Path(args.java_home)
    sdk = Path(args.android_sdk) if args.android_sdk else args.engine_home / 'toolchains/android-sdk'
    tools = sdk / 'build-tools/35.0.0'
    BUILD.mkdir(parents=True, exist_ok=True)
    classes = BUILD / 'classes'; classes.mkdir(exist_ok=True)
    dex = BUILD / 'dex'; dex.mkdir(exist_ok=True)
    platform = sdk / 'platforms/android-30/android.jar'
    sdk_jar = args.mcp_sdk / 'mcp-mobile-client-0.1.0.jar'
    dependencies = sorted((args.mcp_sdk / 'dependency').glob('*.jar'))
    if not sdk_jar.is_file() or not dependencies:
        raise RuntimeError('Build mcp-mobile 0.1.0 and copy its runtime dependencies first.')
    classpath = os.pathsep.join(str(p) for p in [platform, sdk_jar, *dependencies])
    source = sorted((ROOT / 'android/src').rglob('*.java'))
    run(JAVA / 'bin/javac', '-encoding', 'UTF-8', '--release', '8',
        '-classpath', classpath, '-d', classes, *source)
    digest = hashlib.sha256()
    for jar in [sdk_jar, *dependencies]: digest.update(jar.read_bytes())
    cached_dex = BUILD / ('sdk-' + digest.hexdigest()[:16] + '.zip')
    if not cached_dex.exists():
        run(JAVA / 'bin/java', '-Xmx2g', '-cp', tools / 'lib/d8.jar', 'com.android.tools.r8.D8',
            '--intermediate', '--lib', platform, '--min-api', '28', '--output', cached_dex, sdk_jar, *dependencies)
    for old_dex in dex.glob('*.dex'): old_dex.unlink()
    run(JAVA / 'bin/java', '-Xmx2g', '-cp', tools / 'lib/d8.jar', 'com.android.tools.r8.D8',
        '--lib', platform, '--min-api', '28', '--output', dex, *sorted(classes.rglob('*.class')), cached_dex)
    unsigned = BUILD / 'unsigned.apk'
    resources = BUILD / 'resources.zip'
    run(tools / 'aapt2', 'compile', '--dir', ROOT / 'android/res', '-o', resources)
    run(tools / 'aapt2', 'link', '-I', platform, '--manifest', ROOT / 'android/AndroidManifest.xml',
        '-R', resources, '-A', ROOT / 'android/assets', '-o', unsigned)
    if args.compile_only:
        print('Native UI compiled and dexed successfully. Runtime not packaged yet.')
        return
    candidates = list((args.engine_home / 'vendor/llama-android').rglob('llama-server'))
    if len(candidates) != 1:
        raise RuntimeError('Verify and extract the downloaded runtime first using scripts/phone.py verify.')
    binaries = candidates[0].parent
    libraries = sorted((args.engine_home / 'vendor/llama-android').rglob('*.so'))
    if not libraries:
        raise RuntimeError('No runtime libraries found; inspect the upstream archive layout.')
    with zipfile.ZipFile(unsigned, 'a', compression=zipfile.ZIP_DEFLATED) as apk:
        for dex_file in sorted(dex.glob('*.dex')):
            apk.write(dex_file, dex_file.name)
        apk.write(candidates[0], 'lib/arm64-v8a/libaiphone_engine.so')
        speech = args.engine_home / 'vendor/whisper-android/whisper-cli'
        if not speech.is_file():
            raise RuntimeError('Run scripts/build_voice.py to build the offline speech runtime.')
        apk.write(speech, 'lib/arm64-v8a/libaiphone_whisper.so')
        names = set()
        for library in libraries:
            if library.name in names:
                continue
            names.add(library.name)
            apk.write(library.resolve(), 'lib/arm64-v8a/' + library.name)
    aligned = BUILD / 'aligned.apk'
    run(tools / 'zipalign', '-f', '-P', '16', '4', unsigned, aligned)
    private = args.engine_home / 'private'; private.mkdir(mode=0o700, exist_ok=True); private.chmod(0o700)
    keystore = private / 'debug.keystore'
    if not keystore.exists():
        run(JAVA / 'bin/keytool', '-genkeypair', '-keystore', keystore,
            '-storepass', 'android', '-keypass', 'android', '-alias', 'androiddebugkey',
            '-dname', 'CN=AI Phone Local Development', '-keyalg', 'RSA', '-keysize', '2048', '-validity', '3650')
        keystore.chmod(0o600)
    result = ROOT / 'build/cully-debug.apk'
    run(tools / 'apksigner', 'sign', '--ks', keystore, '--ks-pass', 'pass:android',
        '--key-pass', 'pass:android', '--out', result, aligned)
    verification = run(tools / 'apksigner', 'verify', '--verbose', result)
    (ROOT / 'results').mkdir(exist_ok=True)
    (ROOT / 'results/apk-verification.txt').write_text(verification + '\n')
    print(f'Built and signature-verified: {result} ({result.stat().st_size} bytes)')


if __name__ == '__main__':
    main()
