# Security Policy

## Supported Versions

We recommend always using the latest stable release. Older versions may contain known vulnerabilities.

| Version | Supported          |
| ------- | ------------------ |
| latest  | ✅ Active development |

## ⚠️ Important Disclaimers

### Not Production-Ready
GoForge is currently in active development and has not been audited for security. Before deploying:

- Review the code yourself
- Test in a non-production environment
- Understand the security implications of running a self-hosted PaaS

### Security Responsibilities
When running GoForge, you are responsible for:
- Proper network configuration and firewall rules
- Managing secrets and credentials securely
- Regular updates and patching
- Container image security
- Access control and authentication

## Reporting a Vulnerability

### For Security Researchers
We take security vulnerabilities seriously. If you find a security issue:

1. **Do NOT** open a public GitHub issue
2. Email security concerns privately (or use GitHub's private vulnerability reporting)
3. Include detailed steps to reproduce the issue
4. Allow reasonable time for response before public disclosure

### What to Expect
- Acknowledgment of your report within 48 hours
- Regular updates on progress
- Credit in the security advisory (if desired)

## Known Security Gaps (As of Current Version)

The following security features are planned but not yet implemented:

- [ ] Database encryption at rest for sensitive variables
- [ ] Two-factor authentication (2FA)
- [ ] Role-based access control (RBAC)
- [ ] Comprehensive audit logging
- [ ] API key rotation mechanism
- [ ] Container escape prevention measures
- [ ] Resource quota enforcement per project

## Security Best Practices for Deployment

### Minimum Requirements
- [ ] Use **Cloudflare Tunnel** for external access (recommended - no inbound ports needed)
- [ ] Traefik handles internal container routing only (not exposed publicly)
- [ ] Use strong, unique secrets for all credentials
- [ ] Restrict access to Docker socket
- [ ] Enable firewall rules (allow outbound to Cloudflare, block unnecessary inbound)
- [ ] Regular security updates and patches

### Recommended Hardening
- [ ] Enable audit logging
- [ ] Implement network segmentation
- [ ] Use secrets management (Vault, AWS Secrets Manager, etc.)
- [ ] Regular penetration testing
- [ ] Container image scanning in CI/CD

## Third-Party Dependencies

GoForge depends on several third-party libraries. We monitor for vulnerabilities and update dependencies regularly, but users should:
- Monitor CVE announcements for dependencies
- Keep Go version updated
- Review container base images for vulnerabilities
- Use tools like Trivy or Snyk for scanning

## License & Liability

This software is provided "as is" without warranty of any kind. Users assume all risks associated with deployment and operation of this self-hosted platform.
