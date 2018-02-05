FROM scratch

COPY certificate-controller certificate-controller

CMD ["/certificate-controller"]