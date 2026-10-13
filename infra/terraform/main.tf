terraform {
  required_providers {
    vultr = { source = "vultr/vultr", version = "~> 2.21" }
  }
}

# token from VULTR_API_KEY
provider "vultr" {}

data "vultr_os" "ubuntu" {
  filter {
    name   = "name"
    values = ["Ubuntu 24.04 LTS x64"]
  }
}

resource "vultr_ssh_key" "deploy" {
  name    = "sweem-deploy"
  ssh_key = file(pathexpand(var.ssh_public_key))
}

resource "vultr_firewall_group" "sweem" {
  description = "sweem"
}

resource "vultr_firewall_rule" "ssh" {
  firewall_group_id = vultr_firewall_group.sweem.id
  protocol          = "tcp"
  ip_type           = "v4"
  subnet            = split("/", var.ssh_cidr)[0]
  subnet_size       = tonumber(split("/", var.ssh_cidr)[1])
  port              = "22"
}

resource "vultr_firewall_rule" "web" {
  for_each          = toset(["80", "443"])
  firewall_group_id = vultr_firewall_group.sweem.id
  protocol          = "tcp"
  ip_type           = "v4"
  subnet            = "0.0.0.0"
  subnet_size       = 0
  port              = each.value
}

resource "vultr_instance" "sweem" {
  label             = "sweem"
  hostname          = "sweem"
  region            = var.region
  plan              = var.plan
  os_id             = data.vultr_os.ubuntu.id
  ssh_key_ids       = [vultr_ssh_key.deploy.id]
  firewall_group_id = vultr_firewall_group.sweem.id
  backups           = "enabled"
  backups_schedule {
    type = "daily"
  }
}
