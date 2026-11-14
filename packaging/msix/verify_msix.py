"""Validate the actual MSIX payload, identity and block hashes without installing it."""

import argparse
import base64
import hashlib
import json
from pathlib import Path
import struct
import xml.etree.ElementTree as ET
import zipfile


def require(condition, message):
    if not condition:
        raise ValueError(message)


def verify(info_path, store):
    info = json.loads(info_path.read_text(encoding="utf-8-sig"))
    name = info["packageFile"]
    require(Path(name).name == name, "Invalid package filename")
    package = info_path.parent / name
    require(hashlib.sha256(package.read_bytes()).hexdigest().upper() == info["packageSha256"], "Package hash mismatch")
    payload = {
        "AppxManifest.xml", "system-widget.exe", "LICENSE", "PRIVACY.md",
        "THIRD_PARTY_NOTICES.md", "Assets/Square44x44Logo.png",
        "Assets/Square150x150Logo.png", "Assets/StoreLogo.png",
    }
    footprint = {"AppxBlockMap.xml", "[Content_Types].xml"}
    if not store:
        footprint |= {"AppxSignature.p7x", "AppxMetadata/CodeIntegrity.cat"}
    ns = {"a": "http://schemas.microsoft.com/appx/manifest/foundation/windows10"}
    with zipfile.ZipFile(package) as archive:
        require(len(archive.namelist()) == len(set(archive.namelist())), "Duplicate ZIP entries")
        require(set(archive.namelist()) == payload | footprint, "Unexpected or missing package files")
        manifest = ET.fromstring(archive.read("AppxManifest.xml"))
        identity = manifest.find("a:Identity", ns)
        for field, key in (("Name", "packageName"), ("Publisher", "publisher"), ("Version", "version")):
            require(identity.get(field) == info[key], "Manifest identity mismatch: " + field)
        require(identity.get("ProcessorArchitecture") == "x64", "Expected x64 package")
        exe = archive.read("system-widget.exe")
        require(exe.startswith(b"MZ"), "Expected Windows executable")
        for filename, size in (("Square44x44Logo.png", 44), ("Square150x150Logo.png", 150), ("StoreLogo.png", 50)):
            png = archive.read("Assets/" + filename)
            require(png[:8] == b"\x89PNG\r\n\x1a\n" and struct.unpack(">II", png[16:24]) == (size, size), "Invalid icon " + filename)
        blockmap = ET.fromstring(archive.read("AppxBlockMap.xml"))
        require(blockmap.get("HashMethod") == "http://www.w3.org/2001/04/xmlenc#sha256", "Expected SHA-256 blockmap")
        mapped = set()
        count = 0
        for file in blockmap:
            filename = file.get("Name").replace("\\", "/")
            mapped.add(filename)
            data = archive.read(filename)
            require(len(data) == int(file.get("Size")), "Size mismatch: " + filename)
            blocks = file.findall("{http://schemas.microsoft.com/appx/2010/blockmap}Block")
            require(len(blocks) == max(1, (len(data) + 65535) // 65536), "Block count mismatch: " + filename)
            for index, block in enumerate(blocks):
                actual = hashlib.sha256(data[index * 65536:(index + 1) * 65536]).digest()
                require(base64.b64decode(block.get("Hash")) == actual, "Block hash mismatch: " + filename)
                count += 1
        require(mapped == payload, "Blockmap does not cover every payload file")
        if store:
            require(not info["workingTreeDirty"], "Store submission must use committed source")
            expected = json.loads(Path(__file__).with_name("store-identity.json").read_text(encoding="utf-8"))
            require(identity.get("Name") == expected["identityName"] and identity.get("Publisher") == expected["publisher"], "Not the reserved Store identity")
            require(identity.get("Version").endswith(".0"), "Store version must end with .0")
            require(b"widget-go-store" in exe, "Missing isolated Store data directory")
            require(b"widget-go-msix-test/data" not in exe, "Test binary submitted as Store app")
            startup = manifest.find(".//{http://schemas.microsoft.com/appx/manifest/desktop/windows10}StartupTask")
            require(startup is not None and startup.get("Enabled") == "false", "Startup must be disabled by default")
            caps = manifest.find("a:Capabilities", ns)
            require(len(caps) == 1 and caps[0].get("Name") == "runFullTrust", "Unexpected capabilities")
    print(json.dumps({"package": name, "store": store, "payloadFiles": len(payload), "verifiedBlocks": count, "sha256": info["packageSha256"]}))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--info", required=True, type=Path)
    parser.add_argument("--store", action="store_true")
    args = parser.parse_args()
    verify(args.info, args.store)
