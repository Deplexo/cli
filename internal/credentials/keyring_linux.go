package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/godbus/dbus/v5"
)

const service = "org.freedesktop.secrets"
const servicePath = dbus.ObjectPath("/org/freedesktop/secrets")
const serviceInterface = "org.freedesktop.Secret.Service"
const collectionInterface = "org.freedesktop.Secret.Collection"
const itemInterface = "org.freedesktop.Secret.Item"

var errKeyring = errors.New("could not use the OS keyring; unlock your keyring, use DEPLEXO_TOKEN, or explicitly select --insecure-storage")

type keyringVault struct {
	ctx  context.Context
	conn *dbus.Conn
	key  string
}
type secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func openKeyring(ctx context.Context, key string, _ bool) (Vault, func(), error) {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return nil, nil, errKeyring
	}
	return &keyringVault{ctx: ctx, conn: conn, key: key}, func() { _ = conn.Close() }, nil
}

func (v *keyringVault) call(path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(v.ctx, 10*time.Second)
	defer cancel()
	return v.conn.Object(service, path).CallWithContext(ctx, method, 0, args...)
}

func (v *keyringVault) attributes() map[string]string {
	return map[string]string{"service": "deplexo-cli", "profile": v.key}
}

func (v *keyringVault) items() ([]dbus.ObjectPath, error) {
	var unlocked, locked []dbus.ObjectPath
	if err := v.call(servicePath, serviceInterface+".SearchItems", v.attributes()).Store(&unlocked, &locked); err != nil {
		return nil, errKeyring
	}
	// Never trigger a desktop prompt from an ordinary command or --no-input.
	if len(locked) != 0 {
		return nil, errKeyring
	}
	return unlocked, nil
}

func (v *keyringVault) session() (dbus.ObjectPath, error) {
	var output dbus.Variant
	var path dbus.ObjectPath
	if err := v.call(servicePath, serviceInterface+".OpenSession", "plain", dbus.MakeVariant("")).Store(&output, &path); err != nil {
		return "", errKeyring
	}
	return path, nil
}

func (v *keyringVault) Load() (Session, error) {
	items, err := v.items()
	if err != nil {
		return Session{}, err
	}
	if len(items) == 0 {
		return Session{}, ErrNotFound
	}
	if len(items) != 1 {
		return Session{}, ErrMalformed
	}
	path, err := v.session()
	if err != nil {
		return Session{}, err
	}
	defer v.call(path, "org.freedesktop.Secret.Session.Close")
	var value secret
	if err := v.call(items[0], itemInterface+".GetSecret", path).Store(&value); err != nil {
		return Session{}, errKeyring
	}
	return decode(value.Value)
}

func (v *keyringVault) Save(s Session) error {
	data, err := json.Marshal(s)
	if err != nil || len(data) > 16384 {
		return errors.New("could not encode the sign-in")
	}
	items, err := v.items()
	if err != nil {
		return err
	}
	if len(items) > 1 {
		return errors.New("multiple stored sign-ins match this profile; sign out before signing in again")
	}
	path, err := v.session()
	if err != nil {
		return err
	}
	defer v.call(path, "org.freedesktop.Secret.Session.Close")
	value := secret{path, []byte{}, data, "application/json"}
	if len(items) == 1 {
		if v.call(items[0], itemInterface+".SetSecret", value).Err != nil {
			return errKeyring
		}
		return nil
	}
	var collection dbus.ObjectPath
	if err := v.call(servicePath, serviceInterface+".ReadAlias", "default").Store(&collection); err != nil || collection == "/" {
		return errKeyring
	}
	var locked dbus.Variant
	if err := v.call(collection, "org.freedesktop.DBus.Properties.Get", collectionInterface, "Locked").Store(&locked); err != nil || locked.Value() != false {
		return errKeyring
	}
	properties := map[string]dbus.Variant{itemInterface + ".Label": dbus.MakeVariant("Deplexo CLI sign-in"), itemInterface + ".Attributes": dbus.MakeVariant(v.attributes())}
	var item, prompt dbus.ObjectPath
	if err := v.call(collection, collectionInterface+".CreateItem", properties, value, true).Store(&item, &prompt); err != nil || prompt != "/" {
		return errKeyring
	}
	return nil
}

func (v *keyringVault) Delete() error {
	items, err := v.items()
	if err != nil {
		return err
	}
	for _, item := range items {
		var prompt dbus.ObjectPath
		if err := v.call(item, itemInterface+".Delete").Store(&prompt); err != nil || prompt != "/" {
			return errKeyring
		}
	}
	return nil
}
