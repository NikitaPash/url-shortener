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

  # Path 2 capacity run: let the load-generator Droplet reach go-api:8080 directly
  # over the private VPC network (X-Real-IP rotation works, no nginx/TLS overhead).
  # Sourced from the LG Droplet ONLY, and present only while enable_loadgen=true —
  # so disabling load-gen automatically revokes this opening.
  dynamic "inbound_rule" {
    for_each = var.enable_loadgen ? [1] : []
    content {
      protocol           = "tcp"
      port_range         = "8080"
      source_droplet_ids = [digitalocean_droplet.loadgen[0].id]
    }
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

# --- Load-generator Droplet (optional; gated by enable_loadgen) -------------
# A throwaway box that runs k6 against the SUT for the capacity benchmark. Same
# region as the SUT => same default VPC => it reaches the SUT's private IP. Kept
# separate so the generator never steals the server's CPU (the #1 way load tests
# lie). cloud-init pre-installs Docker, clones the repo to /opt/loadtest and pulls
# grafana/k6, so after `terraform apply` you SSH in and run immediately.
resource "digitalocean_droplet" "loadgen" {
  count     = var.enable_loadgen ? 1 : 0
  name      = "url-shortener-loadgen"
  size      = var.loadgen_size
  image     = "ubuntu-24-04-x64"
  region    = var.region # same region as the SUT => shared default VPC
  ssh_keys  = [digitalocean_ssh_key.thesis.id]
  user_data = file("${path.module}/cloud-init-loadgen.yml")

  tags = ["url-shortener-loadgen"]
}

resource "digitalocean_firewall" "loadgen" {
  count       = var.enable_loadgen ? 1 : 0
  name        = "shortener-loadgen-fw"
  droplet_ids = [digitalocean_droplet.loadgen[0].id]

  # SSH in to drive the runs. The generator only makes OUTBOUND requests, so no
  # other inbound ports are needed.
  inbound_rule {
    protocol         = "tcp"
    port_range       = "22"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

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
