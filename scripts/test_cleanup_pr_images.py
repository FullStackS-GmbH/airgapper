"""Destructive-selection regression tests; no network calls or real deletions."""

import contextlib
import copy
import io
import hashlib
import unittest
from urllib.parse import parse_qs, urlparse

from cleanup_pr_images import cleanup, deletion_order, pr_tag


def digest(name):
    return "sha256:" + hashlib.sha256(name.encode()).hexdigest()


def version(identifier, name, *image_tags):
    return {"id": identifier, "name": digest(name), "metadata": {"container": {"tags": list(image_tags)}}}


class FakeAPI:
    def __init__(self, versions, graph, state="closed"):
        self.versions = versions
        self.graph = graph
        self.state = state
        self.deleted = []
        self.requests = []
        self.current = {}
        self.inspection_error = None
        self.missing_package = False

    def login_registry(self, image):
        self.image = image

    def request(self, url, method="GET", registry=False, missing_ok=False):
        self.requests.append((method, url))
        path = urlparse(url).path
        if "/pulls/" in path:
            return {"state": self.state}
        if registry:
            if self.inspection_error:
                raise self.inspection_error
            return {"manifests": [{"digest": d} for d in self.graph[path.rsplit("/", 1)[1]]]}
        if path.endswith("/versions"):
            if self.missing_package:
                return None
            page = int(parse_qs(urlparse(url).query)["page"][0])
            return copy.deepcopy(self.versions[(page - 1) * 100:page * 100])
        identifier = int(path.rsplit("/", 1)[1])
        if method == "DELETE":
            self.deleted.append(identifier)
            return None
        if identifier in self.current:
            return self.current[identifier]
        return next(v for v in self.versions if v["id"] == identifier)


class CleanupTests(unittest.TestCase):
    def setUp(self):
        # Mock deletion messages must not look like real registry operations.
        capture = contextlib.redirect_stdout(io.StringIO())
        capture.__enter__()
        self.addCleanup(capture.__exit__, None, None, None)

    def test_exact_pr_tag_boundaries_and_rerun_tags(self):
        sha = "a" * 40
        for tag in ["pr-48", "pr-48-" + sha, "pr-48-" + sha + "-run-12345-2"]:
            self.assertTrue(pr_tag(tag, 48), tag)
        for tag in ["pr-4", "pr-480", "pr-480-" + sha, "pr-48-release", "v1.8.0", "latest"]:
            self.assertFalse(pr_tag(tag, 48), tag)

    def test_all_historical_builds_and_unshared_children_deleted(self):
        versions = [
            version(1, "root-old", "pr-48-" + "a" * 40),
            version(2, "root-new", "pr-48", "pr-48-" + "b" * 40),
            version(3, "amd64"), version(4, "arm64"), version(5, "attestation"),
            version(6, "release", "latest", "v1.8.0"),
            version(7, "other-pr", "pr-480"), version(8, "orphan"),
        ]
        graph = {v["name"]: set() for v in versions}
        graph[digest("root-old")] = {digest("amd64"), digest("attestation")}
        graph[digest("root-new")] = {digest("amd64"), digest("arm64")}
        api = FakeAPI(versions, graph)
        cleanup(api, "FullStackS-GmbH/airgapper", "Organization", 48)
        self.assertEqual(set(api.deleted), {1, 2, 3, 4, 5})
        self.assertLess(api.deleted.index(1), api.deleted.index(3))
        self.assertLess(api.deleted.index(2), api.deleted.index(3))
        self.assertTrue(any("/orgs/fullstacks-gmbh/" in url for _, url in api.requests))

    def test_shared_and_release_cotagged_manifests_preserved(self):
        versions = [
            version(1, "pr", "pr-48"), version(2, "shared"), version(3, "own"),
            version(4, "release", "v1.8.0"),
            version(5, "cotagged", "pr-48-" + "a" * 40, "stable"),
            version(6, "cotagged-child"), version(7, "other-pr", "pr-49"),
        ]
        graph = {v["name"]: set() for v in versions}
        graph[digest("pr")] = {digest("shared"), digest("own")}
        graph[digest("release")] = {digest("shared")}
        graph[digest("other-pr")] = {digest("shared")}
        graph[digest("cotagged")] = {digest("cotagged-child")}
        api = FakeAPI(versions, graph)
        cleanup(api, "owner/airgapper", "User", 48)
        self.assertEqual(set(api.deleted), {1, 3})
        self.assertTrue(any("/users/owner/" in url for _, url in api.requests))

    def test_pagination_and_no_deletions_during_listing(self):
        versions = [version(i, f"root-{i}", "pr-48-" + f"{i:040x}") for i in range(1, 102)]
        api = FakeAPI(versions, {v["name"]: set() for v in versions})
        cleanup(api, "owner/airgapper", "Organization", 48)
        self.assertEqual(len(api.deleted), 101)
        list_calls = [i for i, (method, url) in enumerate(api.requests) if "?per_page=" in url]
        delete_calls = [i for i, (method, _) in enumerate(api.requests) if method == "DELETE"]
        self.assertEqual(len(list_calls), 2)
        self.assertLess(max(list_calls), min(delete_calls))

    def test_unreadable_retained_manifest_prevents_all_deletion(self):
        versions = [version(1, "pr", "pr-48")]
        api = FakeAPI(versions, {digest("pr"): set()})
        api.inspection_error = RuntimeError("permission denied")
        with self.assertRaises(RuntimeError):
            cleanup(api, "owner/airgapper", "Organization", 48)
        self.assertEqual(api.deleted, [])

    def test_release_tag_added_since_listing_protects_children(self):
        versions = [version(1, "pr", "pr-48"), version(2, "child")]
        api = FakeAPI(versions, {digest("pr"): {digest("child")}, digest("child"): set()})
        api.current[1] = version(1, "pr", "pr-48", "v1.8.0")
        cleanup(api, "owner/airgapper", "Organization", 48)
        self.assertEqual(api.deleted, [])

    def test_reopened_pr_missing_package_and_no_matching_images(self):
        for state, missing in [("open", False), ("closed", True), ("closed", False)]:
            api = FakeAPI([version(1, "release", "v1.8.0")], {digest("release"): set()}, state)
            api.missing_package = missing
            cleanup(api, "owner/airgapper", "Organization", 48)
            self.assertEqual(api.deleted, [])
            self.assertFalse(hasattr(api, "image"), "no registry login needed")

    def test_untagged_external_index_preserves_shared_child(self):
        versions = [version(1, "pr", "pr-48"), version(2, "child"), version(3, "unknown-index")]
        graph = {digest("pr"): {digest("child")}, digest("child"): set(), digest("unknown-index"): {digest("child")}}
        api = FakeAPI(versions, graph)
        cleanup(api, "owner/airgapper", "Organization", 48)
        self.assertEqual(api.deleted, [1])

    def test_cycle_rejected_before_mutation(self):
        versions = [version(1, "pr", "pr-48"), version(2, "child")]
        graph = {digest("pr"): {digest("child")}, digest("child"): {digest("pr")}}
        with self.assertRaises(RuntimeError):
            deletion_order(versions, graph, 48)


if __name__ == "__main__":
    unittest.main()
