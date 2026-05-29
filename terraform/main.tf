# --- SSH Key ---
resource "digitalocean_ssh_key" "thesis" {
  name       = "thesis-key"
  public_key = file(var.ssh_public_key_path)
}

# --- Droplet ---
resource "digitalocean_droplet" "main" {
  name      = "url-shortener"
  size      = var.droplet_size
  image     = "ubuntu-24-04-x64"
  region    = var.region
  ssh_keys  = [digitalocean_ssh_key.thesis.id]
  user_data = file("${path.module}/cloud-init.yml")

  tags = ["url-shortener"]
}

# --- Firewall ---
resource "digitalocean_firewall" "main" {
  name        = "shortener-fw"
  droplet_ids = [digitalocean_droplet.main.id]

  inbound_rule {
    protocol         = "tcp"
    port_range       = "22"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = "80"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  inbound_rule {
    protocol         = "tcp"
    port_range       = "443"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  # Note: observability UIs (Jaeger, Grafana, Prometheus) are reached through
  # nginx sub-paths (/jaeger/, /grafana/, /prometheus/) over 80/443 — their direct
  # ports are intentionally NOT exposed at the edge.

  outbound_rule {
    protocol              = "tcp"
    port_range            = "all"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "udp"
    port_range            = "all"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "icmp"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}

# --- Reserved IP (stable address for DNS A-records) ---
# DigitalOcean routes traffic to the assigned Droplet automatically — no manual
# interface configuration is needed on the server.
resource "digitalocean_reserved_ip" "main" {
  region = var.region
}

resource "digitalocean_reserved_ip_assignment" "main" {
  ip_address = digitalocean_reserved_ip.main.ip_address
  droplet_id = digitalocean_droplet.main.id
}
