"""Delete GHCR versions belonging exclusively to a closed PR.

Only package versions are deleted; blob garbage collection belongs to GHCR.
All manifest references are inspected before any deletion, so untagged platform
and attestation manifests shared with retained images are protected.
"""

import base64
import json
import os
import re
import urllib.error
import urllib.parse
import urllib.request


class API:
    def __init__(self, token, actor):
        self.token = token
        self.actor = actor
        self.registry_token = None

    def request(self, url, method="GET", registry=False, missing_ok=False):
        headers = {"Accept": "application/vnd.github+json"}
        if registry:
            headers = {
                "Accept": ", ".join([
                    "application/vnd.oci.image.index.v1+json",
                    "application/vnd.docker.distribution.manifest.list.v2+json",
                    "application/vnd.oci.image.manifest.v1+json",
                    "application/vnd.docker.distribution.manifest.v2+json",
                ])
            }
            headers["Authorization"] = "Bearer " + self.registry_token
        else:
            headers["Authorization"] = "Bearer " + self.token
            headers["X-GitHub-Api-Version"] = "2022-11-28"
        req = urllib.request.Request(url, method=method, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=60) as response:
                data = response.read()
                return json.loads(data) if data else None
        except urllib.error.HTTPError as error:
            if missing_ok and error.code == 404:
                return None
            # Do not include credentials or server response bodies in CI logs.
            raise RuntimeError(f"{method} API request failed: HTTP {error.code}") from None

    def login_registry(self, image):
        query = urllib.parse.urlencode({"service": "ghcr.io", "scope": f"repository:{image}:pull"})
        basic = base64.b64encode(f"{self.actor}:{self.token}".encode()).decode()
        req = urllib.request.Request(
            "https://ghcr.io/token?" + query,
            headers={"Authorization": "Basic " + basic},
        )
        try:
            with urllib.request.urlopen(req, timeout=60) as response:
                data = json.load(response)
                self.registry_token = data.get("token") or data["access_token"]
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"GHCR authentication failed: HTTP {error.code}") from None


def tags(version):
    return version["metadata"]["container"]["tags"]


def pr_tag(tag, number):
    return tag == f"pr-{number}" or re.fullmatch(rf"pr-{number}-[0-9a-f]{{40}}(?:-run-[0-9]+-[0-9]+)?", tag) is not None


def closure(roots, graph):
    found = set()
    pending = list(roots)
    while pending:
        digest = pending.pop()
        if digest not in found:
            found.add(digest)
            pending.extend(graph[digest])
    return found


def deletion_order(versions, graph, number):
    roots = {v["name"] for v in versions if any(pr_tag(t, number) for t in tags(v))}
    candidates = closure(roots, graph)
    # Untagged manifests outside this PR may themselves be retained image roots.
    # Protect their references too, not just references of release-tagged indexes.
    retained = {v["name"] for v in versions if v["name"] not in candidates or any(not pr_tag(t, number) for t in tags(v))}
    doomed = candidates - closure(retained, graph)
    # Parents must disappear before any of their referenced child manifests.
    incoming = {digest: 0 for digest in doomed}
    for digest in doomed:
        for child in graph[digest] & doomed:
            incoming[child] += 1
    ready = sorted(d for d, count in incoming.items() if count == 0)
    order = []
    while ready:
        digest = ready.pop()
        order.append(digest)
        for child in sorted(graph[digest] & doomed):
            incoming[child] -= 1
            if incoming[child] == 0:
                ready.append(child)
    if len(order) != len(doomed):
        raise RuntimeError("Manifest cycle detected; refusing deletion")
    return order


def cleanup(api, repository, owner_type, number):
    owner, package = repository.lower().split("/", 1)
    owner_path = "orgs" if owner_type == "Organization" else "users"
    base = f"https://api.github.com/{owner_path}/{owner}/packages/container/{urllib.parse.quote(package, safe='')}/versions"
    # Re-check state after acquiring the workflow's shared publishing lock.
    pr = api.request(f"https://api.github.com/repos/{repository}/pulls/{number}")
    if pr["state"] != "closed":
        print(f"PR #{number} is open; skipping cleanup")
        return
    versions = []
    page = 1
    while True:
        batch = api.request(f"{base}?per_page=100&page={page}", missing_ok=True)
        if batch is None:
            if page != 1:
                raise RuntimeError("Package disappeared during pagination; refusing deletion")
            print("No package found; nothing to delete")
            return
        versions.extend(batch)
        if len(batch) < 100:
            break
        page += 1
    if not any(pr_tag(t, number) for v in versions for t in tags(v)):
        print(f"No image versions tagged for PR #{number}")
        return
    api.login_registry(repository.lower())
    graph = {}

    def inspect(digest):
        if digest in graph:
            return
        if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
            raise RuntimeError("Unexpected manifest digest; refusing deletion")
        manifest = api.request(f"https://ghcr.io/v2/{repository.lower()}/manifests/{digest}", registry=True)
        children = {m["digest"] for m in manifest.get("manifests", [])}
        if "subject" in manifest:
            children.add(manifest["subject"]["digest"])
        graph[digest] = children
        for child in children:
            inspect(child)

    # Fail closed if any retained manifest cannot be inspected.
    for version in versions:
        inspect(version["name"])
    order = deletion_order(versions, graph, number)
    by_digest = {v["name"]: v for v in versions}
    protected = set()
    deleted = 0
    for digest in order:
        if digest in protected or digest not in by_digest:
            continue
        version = by_digest[digest]
        current = api.request(f"{base}/{version['id']}", missing_ok=True)
        if current is None:
            continue
        if current["name"] != digest:
            raise RuntimeError("Package version identity changed; refusing deletion")
        if any(not pr_tag(t, number) for t in tags(current)):
            # A release or another PR may have acquired this digest since listing.
            protected.update(closure([digest], graph))
            print(f"Preserving newly tagged version {version['id']}")
            continue
        api.request(f"{base}/{version['id']}", method="DELETE", missing_ok=True)
        deleted += 1
        print(f"Deleted PR #{number} image version {version['id']} ({digest})")
    print(f"Deleted {deleted} versions; release/shared manifests preserved")


def main():
    with open(os.environ["GITHUB_EVENT_PATH"], encoding="utf-8") as stream:
        event = json.load(stream)
    if event.get("action") != "closed" or event.get("pull_request", {}).get("state") != "closed":
        raise RuntimeError("This script only handles closed pull requests")
    number = event["number"]
    if not isinstance(number, int) or number < 1:
        raise RuntimeError("Invalid PR number")
    cleanup(
        API(os.environ["GH_TOKEN"], os.environ["GITHUB_ACTOR"]),
        os.environ["GITHUB_REPOSITORY"],
        event["repository"]["owner"]["type"],
        number,
    )


if __name__ == "__main__":
    main()
