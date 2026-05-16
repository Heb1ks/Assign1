variable "db_password" {
  description = "postgres password"
  type        = string
  sensitive   = true
  default     = "postgres"
}
