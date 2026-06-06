variable "do_token" {
  description = "DigitalOcean API token (Read + Write scope)"
  type        = string
  sensitive   = true
}

variable "ssh_public_key_path" {
  description = "Path to the SSH public key file"
  type        = string
  default     = "~/.ssh/do_thesis.pub"
}

variable "droplet_size" {
  description = "Droplet size slug. The stack has 12 containers and needs ~8 GB RAM minimum."
  type        = string
  # s-4vcpu-8gb  = 4 vCPU, 8 GB RAM, 160 GB SSD — ~$48/month ($0.071/hr). Default.
  # s-8vcpu-16gb = 8 vCPU, 16 GB RAM             — ~$96/month ($0.143/hr). Comfortable headroom.
  # List all slugs: doctl compute size list
  default = "s-4vcpu-8gb"
}

variable "region" {
  description = "DigitalOcean region slug"
  type        = string
  # fra1 = Frankfurt, Germany (good for EU)
  # ams3 = Amsterdam, lon1 = London, nyc3 = New York, sgp1 = Singapore
  # Full list: doctl compute region list
  default = "fra1"
}

# --- Load generator (optional; for the capacity benchmark) ------------------
variable "enable_loadgen" {
  description = <<-EOT
    Create the throwaway k6 load-generator Droplet (same region => same default
    VPC as the SUT) and open the SUT's PRIVATE :8080 to it. A separate generator
    is what makes the capacity number defensible — co-locating it with the stack
    measures the generator, not the server. Spin up only while benchmarking:
      terraform apply -var="enable_loadgen=true"
    Set back to false (default) to destroy it AND auto-close the :8080 rule.
  EOT
  type        = bool
  default     = false
}

variable "loadgen_size" {
  description = "Load-generator Droplet size. Should be >= the SUT so the generator is never the bottleneck; s-4vcpu-8gb is ample for k6 driving thousands of GET/s."
  type        = string
  default     = "s-4vcpu-8gb"
}
