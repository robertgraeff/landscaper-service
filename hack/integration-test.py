#!/usr/bin/env python3

# SPDX-FileCopyrightText: 2022 "SAP SE or an SAP affiliate company and Gardener contributors"
#
# SPDX-License-Identifier: Apache-2.0

import os
import sys
import utils
import yaml
import json
import model.container_registry
import oci.auth as oa

from util import ctx
from subprocess import run

project_root = os.environ["PROJECT_ROOT"]
test_cluster = os.environ["TEST_CLUSTER"]
hosting_cluster = os.environ["HOSTING_CLUSTER"]
target_cluster_provider = os.environ["TARGET_CLUSTER_PROVIDER"]
laas_version = os.environ["LAAS_VERSION"]
laas_repository = os.environ["LAAS_REPOSITORY"]
repo_ctx_base_url = os.environ["REPO_CTX_BASE_URL"]
repo_auth_url = os.environ["REPO_AUTH_URL"]

factory = ctx().cfg_factory()
print(f"Getting kubeconfig for {test_cluster}")
test_cluster_kubeconfig = factory.kubernetes(test_cluster)
print(f"Getting kubeconfig for {hosting_cluster}")
hosting_cluster_kubeconfig = factory.kubernetes(hosting_cluster)

print(f"Getting credentials for {repo_ctx_base_url}")
cr_conf = model.container_registry.find_config(repo_ctx_base_url, oa.Privileges.READONLY)

with (
    utils.TempFileAuto(prefix="test_cluster_kubeconfig_") as test_cluster_kubeconfig_temp_file,
    utils.TempFileAuto(prefix="hosting_cluster_kubeconfig_") as hosting_cluster_kubeconfig_temp_file,
    utils.TempFileAuto(prefix="registry_auth_", suffix=".json") as registry_temp_file
):
    test_cluster_kubeconfig_temp_file.write(yaml.safe_dump(test_cluster_kubeconfig.kubeconfig()))
    test_cluster_kubeconfig_path = test_cluster_kubeconfig_temp_file.switch()

    hosting_cluster_kubeconfig_temp_file.write(yaml.safe_dump(hosting_cluster_kubeconfig.kubeconfig()))
    hosting_cluster_kubeconfig_path = hosting_cluster_kubeconfig_temp_file.switch()

    auth = utils.base64_encode_to_string(cr_conf.credentials().username() + ":" + cr_conf.credentials().passwd())
    auths = {
        "auths": {
            repo_auth_url: {
                "auth": auth
            }
        }
    }

    registry_temp_file.write(json.dumps(auths))
    registry_secrets_path = registry_temp_file.switch()

    command = ["go", "run", "./pkg/main.go",
                "--kubeconfig", test_cluster_kubeconfig_path,
                "--hosting-kubeconfig", hosting_cluster_kubeconfig_path,
                "--provider-type", target_cluster_provider,
                "--laas-version", laas_version,
                "--laas-repository", laas_repository,
                "--registry-secrets", registry_secrets_path]

    print(f"Running integration test with command: {' '.join(command)}")

    mod_path = os.path.join(project_root, "integration-test")
    run = run(command, cwd=mod_path)

    if run.returncode != 0:
        raise EnvironmentError("Integration test exited with errors")
