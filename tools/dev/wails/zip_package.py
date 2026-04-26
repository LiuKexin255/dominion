"""Creates a portable zip package for the Windows agent.

Usage: python3 zip_package.py <output_zip> <staging_dir> <entries...>
  entries format: <dest_path>=<src_path>

Example:
  python3 zip_package.py out.zip /tmp/stage \
    windows-agent.exe=/tmp/stage/windows-agent.exe \
    resources/bin/ffmpeg.exe=/tmp/stage/ffmpeg.exe
"""

import sys
import zipfile
import os
import hashlib
import tempfile


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(8192), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    if len(sys.argv) < 3:
        print("Usage: zip_package.py <output_zip> <staging_dir> [<dest>=<src>...]",
              file=sys.stderr)
        sys.exit(1)

    output_zip = sys.argv[1]
    staging_dir = sys.argv[2]
    entries = sys.argv[3:]

    with zipfile.ZipFile(output_zip, "w", zipfile.ZIP_DEFLATED) as zf:
        for entry in entries:
            if "=" in entry:
                dest_path, src_path = entry.split("=", 1)
            else:
                dest_path = os.path.basename(entry)
                src_path = entry

            if not os.path.isfile(src_path):
                print("Error: source file not found: {}".format(src_path),
                      file=sys.stderr)
                sys.exit(1)

            zf.write(src_path, dest_path)

            if dest_path.endswith("ffmpeg.exe"):
                sha = sha256_file(src_path)
                sha_dest = dest_path + ".sha256"
                tmp = os.path.join(staging_dir, "ffmpeg.exe.sha256")
                with open(tmp, "w") as f:
                    f.write("{}  {}\n".format(sha, os.path.basename(dest_path)))
                zf.write(tmp, sha_dest)

    print("Created {}".format(output_zip))


if __name__ == "__main__":
    main()
