package servicebus

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/servicebus/mgmt/2017-04-01/servicebus"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/internal/clients"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
)

func expandAuthorizationRuleRights(d *pluginsdk.ResourceData) *[]servicebus.AccessRights {
	rights := make([]servicebus.AccessRights, 0)

	if d.Get("listen").(bool) {
		rights = append(rights, servicebus.Listen)
	}

	if d.Get("send").(bool) {
		rights = append(rights, servicebus.SendEnumValue)
	}

	if d.Get("manage").(bool) {
		rights = append(rights, servicebus.Manage)
	}

	return &rights
}

func flattenAuthorizationRuleRights(rights *[]servicebus.AccessRights) (listen, send, manage bool) {
	// zero (initial) value for a bool in go is false

	if rights != nil {
		for _, right := range *rights {
			switch right {
			case servicebus.Listen:
				listen = true
			case servicebus.SendEnumValue:
				send = true
			case servicebus.Manage:
				manage = true
			default:
				log.Printf("[DEBUG] Unknown Authorization Rule Right '%s'", right)
			}
		}
	}

	return listen, send, manage
}

func authorizationRuleSchemaFrom(s map[string]*pluginsdk.Schema) map[string]*pluginsdk.Schema {
	authSchema := map[string]*pluginsdk.Schema{
		"listen": {
			Type:     pluginsdk.TypeBool,
			Optional: true,
			Default:  false,
		},

		"send": {
			Type:     pluginsdk.TypeBool,
			Optional: true,
			Default:  false,
		},

		"manage": {
			Type:     pluginsdk.TypeBool,
			Optional: true,
			Default:  false,
		},

		"primary_key": {
			Type:      pluginsdk.TypeString,
			Computed:  true,
			Sensitive: true,
		},

		"primary_connection_string": {
			Type:      pluginsdk.TypeString,
			Computed:  true,
			Sensitive: true,
		},

		"secondary_key": {
			Type:      pluginsdk.TypeString,
			Computed:  true,
			Sensitive: true,
		},

		"secondary_connection_string": {
			Type:      pluginsdk.TypeString,
			Computed:  true,
			Sensitive: true,
		},

		"primary_connection_string_alias": {
			Type:      pluginsdk.TypeString,
			Computed:  true,
			Sensitive: true,
		},

		"secondary_connection_string_alias": {
			Type:      pluginsdk.TypeString,
			Computed:  true,
			Sensitive: true,
		},
	}
	return azure.MergeSchema(s, authSchema)
}

func authorizationRuleCustomizeDiff(ctx context.Context, d *pluginsdk.ResourceDiff, _ interface{}) error {
	listen, hasListen := d.GetOk("listen")
	send, hasSend := d.GetOk("send")
	manage, hasManage := d.GetOk("manage")

	if !hasListen && !hasSend && !hasManage {
		return fmt.Errorf("One of the `listen`, `send` or `manage` properties needs to be set")
	}

	if manage.(bool) && (!listen.(bool) || !send.(bool)) {
		return fmt.Errorf("if `manage` is set both `listen` and `send` must be set to true too")
	}

	return nil
}

func waitForPairedNamespaceReplication(ctx context.Context, meta interface{}, resourceGroup, namespaceName string, timeout time.Duration) error {
	namespaceClient := meta.(*clients.Client).ServiceBus.NamespacesClient
	namespace, err := namespaceClient.Get(ctx, resourceGroup, namespaceName)

	if !strings.EqualFold(string(namespace.Sku.Name), "Premium") {
		return err
	}

	disasterRecoveryClient := meta.(*clients.Client).ServiceBus.DisasterRecoveryConfigsClient
	disasterRecoveryResponse, err := disasterRecoveryClient.List(ctx, resourceGroup, namespaceName)
	if disasterRecoveryResponse.Values() == nil {
		return err
	}

	if len(disasterRecoveryResponse.Values()) != 1 {
		return err
	}

	aliasName := *disasterRecoveryResponse.Values()[0].Name

	stateConf := &pluginsdk.StateChangeConf{
		Pending:    []string{string(servicebus.Accepted)},
		Target:     []string{string(servicebus.Succeeded)},
		MinTimeout: 30 * time.Second,
		Timeout:    timeout,
		Refresh: func() (interface{}, string, error) {
			read, err := disasterRecoveryClient.Get(ctx, resourceGroup, namespaceName, aliasName)
			if err != nil {
				return nil, "error", fmt.Errorf("wait read Service Bus Namespace Disaster Recovery Configs %q (Namespace %q / Resource Group %q): %v", aliasName, namespaceName, resourceGroup, err)
			}

			if props := read.ArmDisasterRecoveryProperties; props != nil {
				if props.ProvisioningState == servicebus.Failed {
					return read, "failed", fmt.Errorf("replication for Service Bus Namespace Disaster Recovery Configs %q (Namespace %q / Resource Group %q) failed", aliasName, namespaceName, resourceGroup)
				}
				return read, string(props.ProvisioningState), nil
			}

			return read, "nil", fmt.Errorf("waiting for replication error Service Bus Namespace Disaster Recovery Configs %q (Namespace %q / Resource Group %q): provisioning state is nil", aliasName, namespaceName, resourceGroup)
		},
	}

	_, waitErr := stateConf.WaitForStateContext(ctx)
	return waitErr
}
