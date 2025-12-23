#!/bin/bash
set -e

# ECK Config Operator CRD Management Script
# This script helps manage CRDs for the ECK Config Operator

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CRDS_DIR="$(dirname "$SCRIPT_DIR")/crds"
CRDS=(
  "clustersettings.eck-config-operator.freepik.com"
  "indexlifecyclepolicies.eck-config-operator.freepik.com"
  "indexstatemanagements.eck-config-operator.freepik.com"
  "indextemplates.eck-config-operator.freepik.com"
  "snapshotlifecyclepolicies.eck-config-operator.freepik.com"
  "snapshotrepositories.eck-config-operator.freepik.com"
)

COLOR_GREEN='\033[0;32m'
COLOR_YELLOW='\033[1;33m'
COLOR_RED='\033[0;31m'
COLOR_BLUE='\033[0;34m'
COLOR_RESET='\033[0m'

function print_usage() {
  echo "Usage: $0 <command>"
  echo ""
  echo "Commands:"
  echo "  install       Install CRDs"
  echo "  update        Update existing CRDs"
  echo "  delete        Delete CRDs (⚠️  WARNING: deletes all custom resources)"
  echo "  status        Check CRD installation status"
  echo "  verify        Verify CRD integrity"
  echo "  help          Show this help message"
  echo ""
  echo "Examples:"
  echo "  $0 install       # Install CRDs from local files"
  echo "  $0 status        # Check which CRDs are installed"
  echo "  $0 update        # Update CRDs to latest version"
}

function check_kubectl() {
  if ! command -v kubectl &> /dev/null; then
    echo -e "${COLOR_RED}Error: kubectl is not installed${COLOR_RESET}"
    exit 1
  fi
}

function install_crds() {
  echo -e "${COLOR_BLUE}📦 Installing CRDs...${COLOR_RESET}"
  
  if [ ! -d "$CRDS_DIR" ]; then
    echo -e "${COLOR_RED}Error: CRDs directory not found: $CRDS_DIR${COLOR_RESET}"
    exit 1
  fi
  
  kubectl apply -f "$CRDS_DIR"
  
  echo -e "${COLOR_GREEN}✅ CRDs installed successfully${COLOR_RESET}"
}

function update_crds() {
  echo -e "${COLOR_BLUE}🔄 Updating CRDs...${COLOR_RESET}"
  
  if [ ! -d "$CRDS_DIR" ]; then
    echo -e "${COLOR_RED}Error: CRDs directory not found: $CRDS_DIR${COLOR_RESET}"
    exit 1
  fi
  
  kubectl apply -f "$CRDS_DIR"
  
  echo -e "${COLOR_GREEN}✅ CRDs updated successfully${COLOR_RESET}"
}

function delete_crds() {
  echo -e "${COLOR_RED}⚠️  WARNING: This will delete all CRDs and their custom resources!${COLOR_RESET}"
  echo -e "${COLOR_YELLOW}This action cannot be undone.${COLOR_RESET}"
  echo ""
  read -p "Are you sure you want to continue? (type 'yes' to confirm): " confirmation
  
  if [ "$confirmation" != "yes" ]; then
    echo -e "${COLOR_YELLOW}Aborted.${COLOR_RESET}"
    exit 0
  fi
  
  echo -e "${COLOR_BLUE}🗑️  Deleting CRDs...${COLOR_RESET}"
  
  for crd in "${CRDS[@]}"; do
    if kubectl get crd "$crd" &> /dev/null; then
      echo "Deleting $crd..."
      kubectl delete crd "$crd"
    else
      echo "CRD $crd not found, skipping..."
    fi
  done
  
  echo -e "${COLOR_GREEN}✅ CRDs deleted${COLOR_RESET}"
}

function check_status() {
  echo -e "${COLOR_BLUE}📊 Checking CRD status...${COLOR_RESET}"
  echo ""
  
  local all_installed=true
  
  for crd in "${CRDS[@]}"; do
    if kubectl get crd "$crd" &> /dev/null; then
      local version=$(kubectl get crd "$crd" -o jsonpath='{.metadata.annotations.controller-gen\.kubebuilder\.io/version}' 2>/dev/null || echo "unknown")
      local policy=$(kubectl get crd "$crd" -o jsonpath='{.metadata.annotations.helm\.sh/resource-policy}' 2>/dev/null || echo "none")
      echo -e "${COLOR_GREEN}✓${COLOR_RESET} $crd"
      echo "  Version: $version"
      echo "  Policy: $policy"
      
      # Count custom resources
      local resource_type=$(echo "$crd" | cut -d'.' -f1)
      local count=$(kubectl get "$resource_type" --all-namespaces --no-headers 2>/dev/null | wc -l | tr -d ' ')
      echo "  Resources: $count"
      echo ""
    else
      echo -e "${COLOR_RED}✗${COLOR_RESET} $crd (not installed)"
      echo ""
      all_installed=false
    fi
  done
  
  if [ "$all_installed" = true ]; then
    echo -e "${COLOR_GREEN}All CRDs are installed${COLOR_RESET}"
  else
    echo -e "${COLOR_YELLOW}Some CRDs are missing${COLOR_RESET}"
  fi
}

function verify_crds() {
  echo -e "${COLOR_BLUE}🔍 Verifying CRDs...${COLOR_RESET}"
  echo ""
  
  local all_valid=true
  
  for crd in "${CRDS[@]}"; do
    if kubectl get crd "$crd" &> /dev/null; then
      echo -n "Checking $crd... "
      
      # Check if CRD is established
      local established=$(kubectl get crd "$crd" -o jsonpath='{.status.conditions[?(@.type=="Established")].status}')
      
      if [ "$established" = "True" ]; then
        echo -e "${COLOR_GREEN}OK${COLOR_RESET}"
      else
        echo -e "${COLOR_RED}NOT ESTABLISHED${COLOR_RESET}"
        all_valid=false
      fi
    else
      echo -e "${COLOR_RED}✗${COLOR_RESET} $crd not found"
      all_valid=false
    fi
  done
  
  echo ""
  if [ "$all_valid" = true ]; then
    echo -e "${COLOR_GREEN}✅ All CRDs are valid and established${COLOR_RESET}"
  else
    echo -e "${COLOR_RED}❌ Some CRDs have issues${COLOR_RESET}"
    exit 1
  fi
}

# Main script
check_kubectl

case "${1:-}" in
  install)
    install_crds
    ;;
  update)
    update_crds
    ;;
  delete)
    delete_crds
    ;;
  status)
    check_status
    ;;
  verify)
    verify_crds
    ;;
  help|--help|-h)
    print_usage
    ;;
  *)
    echo -e "${COLOR_RED}Error: Unknown command '${1:-}'${COLOR_RESET}"
    echo ""
    print_usage
    exit 1
    ;;
esac

