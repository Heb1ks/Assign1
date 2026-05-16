terraform {
  required_providers {
    docker = {
      source  = "kreuzwerker/docker"
      version = "~> 3.0"
    }
  }

  # remote state so the team shares the same state file
  backend "local" {
    path = "terraform.tfstate"
  }
}

provider "docker" {}

# shared network for all containers
resource "docker_network" "app" {
  name = "sre-network"
}

# postgres for doctor and appointment services
resource "docker_container" "postgres" {
  name  = "postgres"
  image = "postgres:16-alpine"

  env = [
    "POSTGRES_PASSWORD=${var.db_password}",
    "POSTGRES_USER=postgres",
  ]

  ports {
    internal = 5432
    external = 5432
  }

  networks_advanced {
    name = docker_network.app.name
  }
}

# redis for caching and rate limiting
resource "docker_container" "redis" {
  name  = "redis"
  image = "redis:7-alpine"

  ports {
    internal = 6379
    external = 6379
  }

  networks_advanced {
    name = docker_network.app.name
  }
}

# nats message broker used by all services
resource "docker_container" "nats" {
  name  = "nats"
  image = "nats:latest"

  ports {
    internal = 4222
    external = 4222
  }

  networks_advanced {
    name = docker_network.app.name
  }
}
