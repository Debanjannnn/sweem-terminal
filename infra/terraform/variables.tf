variable "region" {
  type    = string
  default = "sgp"
}

# 4 GB builds the Rust executor with the swap Ansible adds; vultr-cli plans list for others
variable "plan" {
  type    = string
  default = "vc2-2c-4gb"
}

variable "ssh_public_key" {
  type    = string
  default = "~/.ssh/id_ed25519.pub"
}

# your IP, e.g. "203.0.113.4/32"
variable "ssh_cidr" {
  type = string
}
