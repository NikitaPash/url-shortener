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
