variable "region" {
  type    = string
  default = "atl"
}

# whole stack idles ~500 MB; images are built on the deploy machine, not here
variable "plan" {
  type    = string
  default = "vc2-1c-1gb"
}

variable "ssh_public_key" {
  type    = string
  default = "~/.ssh/id_ed25519.pub"
}

# your IP, e.g. "203.0.113.4/32"
variable "ssh_cidr" {
  type = string
}
