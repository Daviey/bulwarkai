# ADR 10: Why VPC Service Controls with user token passthrough

Date: 2026-04

## Status

Accepted

## Context

Without a VPC Service Controls perimeter, users with `aiplatform.endpoints.predict` IAM permissions can call Vertex AI directly, bypassing the proxy and its screening entirely. The proxy only works if there is no alternative path to the model.

VPC Service Controls can restrict access to Vertex AI so that only traffic from the Cloud Run VPC subnet is allowed. However, the perimeter must not break user attribution. Vertex AI audit logs must show which human made each request, not just the service account.

## Decision

Enable VPC Service Controls with an ingress rule that allows `ANY_IDENTITY` from the Cloud Run VPC subnet. The perimeter blocks direct access to Vertex AI, Model Armor, and DLP from outside the subnet, but places no restriction on which identity token is used within it.

The proxy continues to forward the caller's OAuth access token (`X-Forwarded-Access-Token`) to Vertex AI in the `Authorization` header. VPC-SC does not inspect or replace this token. Vertex AI receives and audits the end user's identity.

## Consequences

Users with valid IAM permissions cannot call Vertex AI directly because their traffic originates outside the VPC subnet. All traffic must pass through the proxy, which enforces screening.

Vertex AI audit logs retain per-user attribution. The `principalEmail` field shows the human caller, not the service account.

The perimeter is off by default (`vpc_sc_enabled = false`) because it requires an org-level Access Context Manager policy that may not exist in all environments. Enabling it requires setting `vpc_sc_enabled = true`, `access_policy_name`, and `org_id` in `terraform.tfvars`.
