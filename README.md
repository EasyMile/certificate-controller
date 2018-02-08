# Certificate Controller

## Context

A kubernetes controller for handling the
`service.beta.kubernetes.io/aws-load-balancer-ssl-cert` annotation on
services. This annotation allows kubernetes to bind an
[ACM](https://aws.amazon.com/certificate-manager/) certificate to the
service [ELB](https://aws.amazon.com/elasticloadbalancing/) to provide a
tls termination at the
[ELB](https://aws.amazon.com/elasticloadbalancing/) level.
The `service.beta.kubernetes.io/aws-load-balancer-ssl-cert` takes an
[ARN](https://docs.aws.amazon.com/general/latest/gr/aws-arns-and-namespaces.html)
as value.

In order to avoid each service declaration to be aware of ARNs, we
create this certificate-controller.
Its purpose is to watch for service annoted with `easymile.com/certificate-controller.class` and annotate them with the right ARN found in AWS.

## Usage

It takes two environment variables as parameters:
- `CERTIFICATE_CONTROLLER_CERT_ARN`: the AWS ARN of the ACM certificate to
  associate to the service load balancer.
- `CERTIFICATE_CONTROLLER_CLASS`: the identifier for this controller
  (default: certificate-controller). The controller will watch for services with annotation `easymile.com/certificate-controller.class` matching this identifier. This allow to run multiple controller with different class and ARNs.

