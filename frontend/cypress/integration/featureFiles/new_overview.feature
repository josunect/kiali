@overview
Feature: New Overview - Overview cards

  Background:
    Given user is at administrator perspective

  @core-2
  Scenario: Istio configs card can navigate to Istio config list with all namespaces
    Given Istio configs API is observed
    And user is at the "overview" page
    When user clicks View Istio config in Istio configs card
    Then user is redirected to Istio config list with all namespaces

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
    Given Control planes API fails once
    And user is at the "overview" page
    Then Control planes card shows error state without count or footer link
    When user clicks Try Again in Control planes card
    Then Control planes card shows count and footer link

  @core-2
  Scenario: Control planes issues (if any) can navigate to Mesh page with cluster filter
    Given Control planes API is observed
    And user is at the "overview" page
    When user navigates to Mesh page from Control planes card
    Then user is redirected to Mesh page

  @core-2
  Scenario: Data planes footer link navigates to Namespaces list with type filter
    Given user is at the "overview" page
    When user clicks View Data planes in Data planes card
    Then user is redirected to Namespaces page with data-plane type filter
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
    Given Service insights APIs fail once
    And user is at the "overview" page
    Then Service insights card shows error state without tables or footer link
    When user clicks Try Again in Service insights card
    Then Service insights card shows data tables and footer link

  @core-2
  Scenario: Service insights footer link navigates to Services list with all namespaces and sort
    Given Service insights APIs are observed
    And user is at the "overview" page
    When user clicks View all services in Service insights card
    Then user is redirected to Services list with all namespaces and service insights sorting

  @selected
  @core-2
  Scenario: Service insights service link navigates to service details
    Given Service insights APIs are observed
    And user is at the "overview" page
    When user clicks a valid service link in Service insights card
    Then user is redirected to that Service details page

