#!/usr/bin/env python3
"""Build the pinned ARM64 Whisper executable outside the source checkout."""
import argparse
from pathlib import Path
import shutil
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--engine-home', type=Path, required=True)
parser.add_argument('--android-sdk', type=Path, required=True)
args = parser.parse_args()
root = args.engine_home.resolve()
source = root / 'vendor/whisper.cpp-1.9.5'
commit = subprocess.check_output(['git', '-C', str(source), 'rev-parse', 'HEAD'], text=True).strip()
if commit != 'd1be6fde11ac6e0407606b4e42fe72d34add8037':
    raise SystemExit('Unexpected whisper.cpp revision; clone and verify v1.9.5 first.')
ndks = sorted((args.android_sdk / 'ndk').glob('*/build/cmake/android.toolchain.cmake'))
if not ndks:
    raise SystemExit('Install an Android NDK under the supplied SDK directory.')
build = root / 'build/whisper-android'
subprocess.run(['cmake', '-S', str(source), '-B', str(build), '-G', 'Ninja',
    '-DCMAKE_TOOLCHAIN_FILE=' + str(ndks[-1].resolve()), '-DANDROID_ABI=arm64-v8a',
    '-DANDROID_PLATFORM=android-28', '-DANDROID_STL=c++_static',
    '-DCMAKE_BUILD_TYPE=Release', '-DBUILD_SHARED_LIBS=OFF',
    '-DGGML_NATIVE=OFF', '-DGGML_OPENMP=OFF', '-DGGML_CPU_KLEIDIAI=OFF',
    '-DGGML_VULKAN=OFF', '-DWHISPER_BUILD_TESTS=OFF', '-DWHISPER_BUILD_SERVER=OFF'], check=True)
subprocess.run(['cmake', '--build', str(build), '--target', 'whisper-cli', '-j4'], check=True)
target = root / 'vendor/whisper-android/whisper-cli'
target.parent.mkdir(parents=True, exist_ok=True)
shutil.copy2(build / 'bin/whisper-cli', target)
print('Whisper runtime prepared outside the source checkout.')
