// Package protocol holds the wire-level constants shared by the agent and the
// host client, so the CLI doesn't have to import the agent (and its capture and
// input dependencies) to speak to it.
package protocol

// ClientHeader must be present on every request to an agent. The host client
// always sets it; a browser cannot attach a custom header to a cross-origin
// request without a CORS preflight, which the agent never answers.
const ClientHeader = "X-Vmrc-Client"
