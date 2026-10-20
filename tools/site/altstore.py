#!/usr/bin/env python3
"""Writes the AltStore source (also read by SideStore and LiveContainer) with
every release that has the unsigned iOS app: site/altstore.json, published on
the website. Usage: tools/site/altstore.py <output.json>

It reads the releases from the GitHub API (with GITHUB_TOKEN if set, to avoid
the anonymous rate limit). Each version links to the release's IPA, so adding
the source in AltStore/SideStore offers every new release as an update.
"""
import json
import os
import re
import sys
import urllib.request

REPO = "juanvillacortac/audiofilus"
SITE = "https://juanvillacortac.github.io/audiofilus"
IPA = "audiofilus-ios-unsigned.ipa"
# Before 0.7.0 some releases' IPAs carried an older version inside (v0.4.1
# had 0.4.0), and AltStore rejects a version that doesn't match: the source
# starts at 0.7.0, from when the iOS build takes its version from the tag.
FIRST = (0, 7, 0)


def releases():
    req = urllib.request.Request(f"https://api.github.com/repos/{REPO}/releases?per_page=100",
                                 headers={"Accept": "application/vnd.github+json"})
    if os.environ.get("GITHUB_TOKEN"):
        req.add_header("Authorization", "Bearer " + os.environ["GITHUB_TOKEN"])
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


def notes(body):
    """The release notes as plain text: no images, HTML or markdown marks."""
    body = re.sub(r"<[^>]+>", "", body or "")
    body = re.sub(r"!\[[^\]]*\]\([^)]*\)", "", body)
    body = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", body)
    body = re.sub(r"[*`#]", "", body)
    return re.sub(r"\n{3,}", "\n\n", body).strip()


def main(out):
    versions = []
    for rel in releases():
        if rel.get("draft") or rel.get("prerelease"):
            continue
        ipa = next((a for a in rel.get("assets", []) if a["name"] == IPA), None)
        try:
            num = tuple(int(x) for x in rel["tag_name"].lstrip("v").split("."))
        except ValueError:
            continue
        if not ipa or num < FIRST:
            continue
        versions.append({
            "version": rel["tag_name"].lstrip("v"),
            "buildVersion": "1",
            "date": rel["published_at"],
            "localizedDescription": notes(rel.get("body")),
            "downloadURL": ipa["browser_download_url"],
            "size": ipa["size"],
            "minOSVersion": "15.0",
        })
    source = {
        "name": "Audiofilus",
        "identifier": "io.github.juanvillacortac.audiofilus.source",
        "subtitle": "Pixel-art music player",
        "description": "Releases of Audiofilus for iPhone and iPad.",
        "iconURL": f"{SITE}/img/icon-256.png",
        "website": SITE,
        "tintColor": "#E8503A",
        "apps": [{
            "name": "Audiofilus",
            "bundleIdentifier": "com.juanvillacortac.audiofilus",
            "developerName": "Juan Villacorta",
            "subtitle": "Pixel-art music player",
            "localizedDescription": "A pixel-art music player: your music, Subsonic/Navidrome and Jellyfin servers, internet radio, podcasts and YouTube Music, with synced lyrics, an equalizer, visualizers, games and a chiptune tracker. Keeps playing in the background, with lock screen controls.",
            "iconURL": f"{SITE}/img/icon-256.png",
            "tintColor": "#E8503A",
            "category": "entertainment",
            "screenshots": [f"{SITE}/img/phone-player.png", f"{SITE}/img/phone-library.png"],
            "versions": versions,
            "appPermissions": {"entitlements": [], "privacy": {}},
        }],
        "news": [],
    }
    with open(out, "w") as f:
        json.dump(source, f, indent=2)
    print(f"{out}: {len(versions)} versions")


if __name__ == "__main__":
    main(sys.argv[1])
