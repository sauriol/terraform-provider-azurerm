package serviceconnector

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-azure-helpers/lang/response"
	"github.com/hashicorp/go-azure-sdk/resource-manager/servicelinker/2022-05-01/links"
	"github.com/hashicorp/go-azure-sdk/resource-manager/servicelinker/2022-05-01/servicelinker"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-provider-azurerm/helpers/azure"
	"github.com/hashicorp/terraform-provider-azurerm/internal/sdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/services/web/validate"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/pluginsdk"
	"github.com/hashicorp/terraform-provider-azurerm/internal/tf/validation"
	"github.com/hashicorp/terraform-provider-azurerm/utils"
)

type AppServiceConnectorResource struct{}

type AppServiceConnectorResourceModel struct {
	Name             string          `tfschema:"name"`
	AppServiceId     string          `tfschema:"app_service_id"`
	TargetResourceId string          `tfschema:"target_resource_id"`
	ClientType       string          `tfschema:"client_type"`
	AuthInfo         []AuthInfoModel `tfschema:"authentication"`
	VnetSolution     string          `tfschema:"vnet_solution"`
}

func (r AppServiceConnectorResource) Arguments() map[string]*schema.Schema {
	return map[string]*schema.Schema{
		"name": {
			Type:         pluginsdk.TypeString,
			Required:     true,
			ForceNew:     true,
			ValidateFunc: validation.StringIsNotEmpty,
		},

		"app_service_id": {
			Type:         pluginsdk.TypeString,
			Required:     true,
			ForceNew:     true,
			ValidateFunc: validate.AppServiceID,
		},

		"target_resource_id": {
			Type:         pluginsdk.TypeString,
			Required:     true,
			ForceNew:     true,
			ValidateFunc: azure.ValidateResourceID,
		},

		"client_type": {
			Type:     pluginsdk.TypeString,
			Optional: true,
			Default:  string(servicelinker.ClientTypeNone),
			ValidateFunc: validation.StringInSlice([]string{
				string(servicelinker.ClientTypeNone),
				string(servicelinker.ClientTypeDotnet),
				string(servicelinker.ClientTypeJava),
				string(servicelinker.ClientTypePython),
				string(servicelinker.ClientTypeGo),
				string(servicelinker.ClientTypePhp),
				string(servicelinker.ClientTypeRuby),
				string(servicelinker.ClientTypeDjango),
				string(servicelinker.ClientTypeNodejs),
				string(servicelinker.ClientTypeSpringBoot),
			}, false),
		},

		"vnet_solution": {
			Type:     pluginsdk.TypeString,
			Optional: true,
			Default:  string(servicelinker.VNetSolutionTypePrivateLink),
			ValidateFunc: validation.StringInSlice([]string{
				string(servicelinker.VNetSolutionTypeServiceEndpoint),
				string(servicelinker.VNetSolutionTypePrivateLink),
			}, false),
		},

		"authentication": authInfoSchema(),
	}
}

func (r AppServiceConnectorResource) Attributes() map[string]*schema.Schema {
	return map[string]*pluginsdk.Schema{}
}

func (r AppServiceConnectorResource) ModelObject() interface{} {
	return &AppServiceConnectorResourceModel{}
}

func (r AppServiceConnectorResource) ResourceType() string {
	return "azurerm_app_service_connection"
}

func (r AppServiceConnectorResource) Create() sdk.ResourceFunc {
	return sdk.ResourceFunc{
		Timeout: 30 * time.Minute,
		Func: func(ctx context.Context, metadata sdk.ResourceMetaData) error {
			var model AppServiceConnectorResourceModel
			if err := metadata.Decode(&model); err != nil {
				return err
			}

			client := metadata.Client.ServiceConnector.ServiceLinkerClient

			id := servicelinker.NewScopedLinkerID(model.AppServiceId, model.Name)
			existing, err := client.LinkerGet(ctx, id)
			if err != nil && !response.WasNotFound(existing.HttpResponse) {
				return fmt.Errorf("checking for presence of existing %s: %+v", id, err)
			}

			if !response.WasNotFound(existing.HttpResponse) {
				return metadata.ResourceRequiresImport(r.ResourceType(), id)
			}

			authInfo, err := expandServiceConnectorAuthInfo(model.AuthInfo)
			if err != nil {
				return fmt.Errorf("expanding `authentication`: %+v", err)
			}

			serviceConnectorProperties := servicelinker.LinkerProperties{
				AuthInfo: authInfo,
				TargetService: servicelinker.AzureResource{
					Id: &model.TargetResourceId,
				},
			}

			if model.ClientType != "" {
				clientType := servicelinker.ClientType(model.ClientType)
				serviceConnectorProperties.ClientType = &clientType
			}

			if model.VnetSolution != "" {
				vNetSolutionType := servicelinker.VNetSolutionType(model.VnetSolution)
				vNetSolution := servicelinker.VNetSolution{
					Type: &vNetSolutionType,
				}
				serviceConnectorProperties.VNetSolution = &vNetSolution
			}

			props := servicelinker.LinkerResource{
				Id:         utils.String(id.ID()),
				Name:       utils.String(model.Name),
				Properties: serviceConnectorProperties,
			}

			if _, err = client.LinkerCreateOrUpdate(ctx, id, props); err != nil {
				return fmt.Errorf("creating %s: %+v", id, err)
			}

			metadata.SetID(id)
			return nil
		},
	}
}

func (r AppServiceConnectorResource) Read() sdk.ResourceFunc {
	return sdk.ResourceFunc{
		Timeout: 5 * time.Minute,
		Func: func(ctx context.Context, metadata sdk.ResourceMetaData) error {
			client := metadata.Client.ServiceConnector.ServiceLinkerClient
			id, err := servicelinker.ParseScopedLinkerID(metadata.ResourceData.Id())
			if err != nil {
				return err
			}

			resp, err := client.LinkerGet(ctx, *id)
			if err != nil {
				if response.WasNotFound(resp.HttpResponse) {
					return metadata.MarkAsGone(id)
				}
				return fmt.Errorf("reading %s: %+v", *id, err)
			}

			if model := resp.Model; model != nil {
				props := model.Properties
				if props.AuthInfo == nil || props.TargetService == nil {
					return nil
				}

				state := AppServiceConnectorResourceModel{
					Name:             id.LinkerName,
					AppServiceId:     id.ResourceUri,
					TargetResourceId: flattenTargetService(props.TargetService),
					AuthInfo:         flattenServiceConnectorAuthInfo(props.AuthInfo),
				}

				if props.ClientType != nil {
					state.ClientType = string(*props.ClientType)
				}

				if props.VNetSolution != nil && props.VNetSolution.Type != nil {
					state.VnetSolution = string(*props.VNetSolution.Type)
				}

				return metadata.Encode(&state)
			}
			return nil
		},
	}
}

func (r AppServiceConnectorResource) Delete() sdk.ResourceFunc {
	return sdk.ResourceFunc{
		Timeout: 30 * time.Minute,
		Func: func(ctx context.Context, metadata sdk.ResourceMetaData) error {
			client := metadata.Client.ServiceConnector.LinksClient
			id, err := links.ParseScopedLinkerID(metadata.ResourceData.Id())
			if err != nil {
				return err
			}

			metadata.Logger.Infof("deleting %s", *id)

			if resp, err := client.LinkerDelete(ctx, *id); err != nil {
				if !response.WasNotFound(resp.HttpResponse) {
					return fmt.Errorf("deleting %s: %+v", *id, err)
				}
			}
			return nil
		},
	}
}

func (r AppServiceConnectorResource) Update() sdk.ResourceFunc {
	return sdk.ResourceFunc{
		Timeout: 30 * time.Minute,
		Func: func(ctx context.Context, metadata sdk.ResourceMetaData) error {
			client := metadata.Client.ServiceConnector.LinksClient
			id, err := links.ParseScopedLinkerID(metadata.ResourceData.Id())
			if err != nil {
				return err
			}

			var state AppServiceConnectorResourceModel
			if err := metadata.Decode(&state); err != nil {
				return fmt.Errorf("decoding %+v", err)
			}

			linkerProps := links.LinkerProperties{}
			d := metadata.ResourceData

			if d.HasChange("client_type") {
				clientType := links.ClientType(state.ClientType)
				linkerProps.ClientType = &clientType
			}

			if d.HasChange("vnet_solution") {
				vnetSolutionType := links.VNetSolutionType(state.VnetSolution)
				vnetSolution := links.VNetSolution{
					Type: &vnetSolutionType,
				}
				linkerProps.VNetSolution = &vnetSolution
			}

			if d.HasChange("authentication") {
				linkerProps.AuthInfo = state.AuthInfo
			}

			props := links.LinkerPatch{
				Properties: &linkerProps,
			}

			if _, err := client.LinkerUpdate(ctx, *id, props); err != nil {
				return fmt.Errorf("updating %s: %+v", *id, err)
			}
			return nil
		},
	}
}

func (r AppServiceConnectorResource) IDValidationFunc() pluginsdk.SchemaValidateFunc {
	return servicelinker.ValidateScopedLinkerID
}
