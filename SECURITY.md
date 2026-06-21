# Security Policy

## Reporting a Vulnerability

Please report security vulnerabilities by emailing **spbve1fu6@mozmail.com**.

Do not open public GitHub issues for security vulnerabilities.

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| latest  | :white_check_mark: |
| < latest| :x:                |

## Scope

This project handles secrets in Kubernetes. While chur uses tmpfs (in-memory)
storage to minimize exposure, it is not a replacement for proper secret
management practices (rotation, audit, access control).
