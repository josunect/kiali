@overview
Feature: New Overview - Overview cards

  Background:
    Given user is at administrator perspective

  @core-2
  Scenario: View all warning Istio configs includes namespaces and filters
    Given Istio configs API returns at least 4 warning configs
    And user is at the "overview" page
    When user opens the Istio configs warnings popover
    And user clicks the "View warning Istio configs" popover action
    Then user is redirected to Istio config list with all namespaces and warning filters

  @core-2
  Scenario: Control planes card shows loading state without count or footer link
    Given Control planes API responds slowly
    And user is at the "overview" page
    Then Control planes card shows loading state without count or footer link

  @core-2
  Scenario: Control planes card shows error state with Try Again without count or footer link
    Given Control planes API fails
    And user is at the "overview" page
    Then Control planes card shows error state without count or footer link

  @core-2
  Scenario: Control planes card can retry after error
    Given Control planes API fails
    And user is at the "overview" page
    Then Control planes card shows error state without count or footer link
    When Control planes API succeeds with 1 healthy control plane
    And user clicks Try Again in Control planes card
    Then Control planes card shows count 1 and footer link

  @core-2
  Scenario: Control plane links in popover navigate to Mesh page with cluster filter
    Given Control planes API returns 1 unhealthy control plane in cluster "Kubernetes"
    And user is at the "overview" page
    When user opens the Control planes issues popover
    And user clicks the "istiod-kubernetes" control plane link in the popover
    Then user is redirected to Mesh page with cluster filter "Kubernetes"

  @core-2
  Scenario: Service insights card shows loading state without tables or footer link
    Given Service insights APIs respond slowly
    And user is at the "overview" page
    Then Service insights card shows loading state without tables or footer link

  @core-2
  Scenario: Service insights card shows error state without tables or footer link
    Given Service insights APIs fail
    And user is at the "overview" page
    Then Service insights card shows error state without tables or footer link

  @core-2
  Scenario: Service insights card can retry after error
    Given Service insights APIs fail
    And user is at the "overview" page
    Then Service insights card shows error state without tables or footer link
    When Service insights APIs succeed with data
    And user clicks Try Again in Service insights card
    Then Service insights card shows data tables and footer link

  @core-2
  Scenario: Service insights footer link navigates to Services list with all namespaces and sort
    Given Service insights APIs succeed with no data
    And user is at the "overview" page
    When user clicks View all services in Service insights card
    Then user is redirected to Services list with all namespaces and service insights sorting

  @core-2
  Scenario: Service insights service link navigates to service details with cluster parameter
    Given Service insights APIs succeed with data
    And user is at the "overview" page
    When user clicks the "svc-a" service link in Service insights card
    Then user is redirected to Service details for namespace "alpha" service "svc-a" in cluster "Kubernetes"

