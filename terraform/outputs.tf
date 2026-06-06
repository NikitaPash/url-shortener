output "droplet_ip" {
  description = "Droplet's primary public IPv4 address"
  value       = digitalocean_droplet.main.ipv4_address
}

output "reserved_ip" {
  description = "Stable reserved IP for DNS A-records (survives Droplet recreation)"
  value       = digitalocean_reserved_ip.main.ip_address
}

output "droplet_status" {
  description = "Current Droplet status"
  value       = digitalocean_droplet.main.status
}

# --- Load-generator outputs (null unless enable_loadgen=true) ----------------
output "loadgen_ip" {
  description = "Public IPv4 of the load-generator Droplet — your SSH target for the run."
  value       = one(digitalocean_droplet.loadgen[*].ipv4_address)
}

output "loadgen_private_ip" {
  description = "Private (VPC) IPv4 of the load generator."
  value       = one(digitalocean_droplet.loadgen[*].ipv4_address_private)
}

output "sut_private_ip" {
  description = "Private (VPC) IPv4 of the SUT — set as SUT_PRIVATE_IP for the Path 2 direct-:8080 run."
  value       = digitalocean_droplet.main.ipv4_address_private
}
