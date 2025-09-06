FROM scratch
LABEL authors="leganck"
COPY  fn-qb-http /
ENV  UDS="/app/qbt.sock" \
     PORT=18080 \
     PASSWORD="admin"

CMD ["/fn-qb-http"]