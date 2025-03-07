# SPDX-FileCopyrightText: 2021 "SAP SE or an SAP affiliate company and Gardener contributors"
#
# SPDX-License-Identifier: Apache-2.0

#### BUILDER ####
FROM golang:1.18.6 AS builder

WORKDIR /go/src/github.com/gardener/landscaper-service
COPY . .

ARG EFFECTIVE_VERSION

RUN make install EFFECTIVE_VERSION=$EFFECTIVE_VERSION

#### BASE ####
FROM gcr.io/distroless/static-debian11:nonroot AS base

#### Landscaper Service controller ####
FROM base as landscaper-service-controller

COPY --from=builder /go/bin/landscaper-service-controller /landscaper-service-controller

WORKDIR /

ENTRYPOINT ["/landscaper-service-controller"]

#### Landscaper Service webhooks server ####
FROM base as landscaper-service-webhooks-server

COPY --from=builder /go/bin/landscaper-service-webhooks-server /landscaper-service-webhooks-server

WORKDIR /

ENTRYPOINT ["/landscaper-service-webhooks-server"]
