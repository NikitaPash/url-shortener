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
